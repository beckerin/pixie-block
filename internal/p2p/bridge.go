package p2p

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/beckerin/pixie-block/internal/chain"
	"github.com/beckerin/pixie-block/internal/domain"
	"github.com/beckerin/pixie-block/internal/ledger"
	"github.com/beckerin/pixie-block/internal/mempool"
)

// ErrNeedSync is returned when a peer announces a block beyond the next height.
var ErrNeedSync = errors.New("need chain sync")

type Bridge struct {
	chainID    string
	nodeID     string
	chain      *chain.Blockchain
	mempool    *mempool.Pool
	createPool *mempool.AccountCreatePool
	node       *Node
	producer   BlockProducer

	mu sync.Mutex
}

type BlockProducer interface {
	CanProduce(height int64) bool
	CreateBlock(height int64, prev domain.Block, txs []domain.PaymentTransaction, creates []domain.AccountCreateTransaction) (domain.Block, error)
}

func NewBridge(chainID, nodeID string, bc *chain.Blockchain, pool *mempool.Pool, createPool *mempool.AccountCreatePool, producer BlockProducer, listenAddr string, peers []string) *Bridge {
	b := &Bridge{
		chainID:    chainID,
		nodeID:     nodeID,
		chain:      bc,
		mempool:    pool,
		createPool: createPool,
		producer:   producer,
	}
	b.node = New(listenAddr, peers, b)
	return b
}

func (b *Bridge) Start() error {
	return b.node.Start()
}

func (b *Bridge) BroadcastTransaction(tx domain.PaymentTransaction) {
	b.node.Broadcast(MsgNewTransaction, tx)
}

func (b *Bridge) BroadcastAccountCreate(tx domain.AccountCreateTransaction) {
	b.node.Broadcast(MsgNewAccountCreate, tx)
}

func (b *Bridge) BroadcastBlock(block domain.Block) {
	b.node.Broadcast(MsgNewBlock, block)
}

func (b *Bridge) ChainID() string      { return b.chainID }
func (b *Bridge) NodeID() string       { return b.nodeID }
func (b *Bridge) CurrentHeight() int64 { return b.chain.Height() }

func (b *Bridge) OnNewTransaction(data json.RawMessage) error {
	var tx domain.PaymentTransaction
	if err := json.Unmarshal(data, &tx); err != nil {
		return err
	}
	return b.mempool.TryAdd(tx, func(reserved int64) error {
		return b.chain.ValidateAdmit(tx, reserved)
	})
}

func (b *Bridge) OnNewAccountCreate(data json.RawMessage) error {
	var tx domain.AccountCreateTransaction
	if err := json.Unmarshal(data, &tx); err != nil {
		return err
	}
	return b.createPool.TryAdd(tx, func() error {
		return b.chain.ValidateAccountCreate(tx)
	})
}

func (b *Bridge) OnNewBlock(data json.RawMessage) error {
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}

	switch blocks := payload.(type) {
	case map[string]any:
		if _, ok := blocks["blocks"]; ok {
			var resp struct {
				Blocks []domain.Block `json:"blocks"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				return err
			}
			return b.applyBlocks(resp.Blocks)
		}
	}

	var block domain.Block
	if err := json.Unmarshal(data, &block); err != nil {
		return err
	}
	return b.applyBlocks([]domain.Block{block})
}

func (b *Bridge) OnGetBlocks(fromHeight int64) (json.RawMessage, error) {
	blocks := b.chain.GetBlocksFrom(fromHeight)
	resp := struct {
		Blocks []domain.Block `json:"blocks"`
	}{Blocks: blocks}
	return json.Marshal(resp)
}

func (b *Bridge) applyBlocks(blocks []domain.Block) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	applied := 0
	for _, block := range blocks {
		if block.Height <= b.chain.Height() {
			continue
		}
		if block.Height > b.chain.Height()+1 {
			return fmt.Errorf("%w: got height %d, local %d", ErrNeedSync, block.Height, b.chain.Height())
		}
		txIDs := make([]string, len(block.Transactions))
		for i, tx := range block.Transactions {
			txIDs[i] = tx.ID
		}
		createIDs := make([]string, len(block.AccountCreates))
		for i, tx := range block.AccountCreates {
			createIDs[i] = tx.ID
		}
		if err := b.mempool.Commit(txIDs, func() error {
			return b.createPool.Commit(createIDs, func() error {
				return b.chain.AppendBlock(block)
			})
		}); err != nil {
			return err
		}
		applied++
	}
	if applied > 0 {
		log.Printf("p2p applied %d block(s), height now %d", applied, b.chain.Height())
	}
	return nil
}

func (b *Bridge) ProduceBlockIfReady() (domain.Block, bool, error) {
	if b.producer == nil {
		return domain.Block{}, false, nil
	}

	height := b.chain.Height() + 1
	if !b.producer.CanProduce(height) {
		return domain.Block{}, false, nil
	}

	maxTxs := b.chain.Genesis().MaxTxsPerBlock
	if maxTxs <= 0 {
		maxTxs = 100
	}
	txs, creates := b.selectExecutable(maxTxs)
	if len(txs) == 0 && len(creates) == 0 {
		return domain.Block{}, false, nil
	}

	prev := b.chain.LatestBlock()
	block, err := b.producer.CreateBlock(height, prev, txs, creates)
	if err != nil {
		return domain.Block{}, false, err
	}

	txIDs := make([]string, len(txs))
	for i, tx := range txs {
		txIDs[i] = tx.ID
	}
	createIDs := make([]string, len(creates))
	for i, tx := range creates {
		createIDs[i] = tx.ID
	}
	if err := b.mempool.Commit(txIDs, func() error {
		return b.createPool.Commit(createIDs, func() error {
			return b.chain.AppendBlock(block)
		})
	}); err != nil {
		return domain.Block{}, false, err
	}

	b.BroadcastBlock(block)
	return block, true, nil
}

// selectExecutable picks up to max account creates and payment txs that apply
// cleanly in order against confirmed state (creates first).
func (b *Bridge) selectExecutable(max int) ([]domain.PaymentTransaction, []domain.AccountCreateTransaction) {
	state := b.chain.State()
	creates := make([]domain.AccountCreateTransaction, 0)
	var dropCreates []string

	for _, tx := range b.createPool.Peek(0) {
		if len(creates) >= max {
			break
		}
		if err := ledger.ApplyAccountCreate(tx, state); err != nil {
			dropCreates = append(dropCreates, tx.ID)
			log.Printf("dropping invalid mempool account create %s: %v", tx.ID, err)
			continue
		}
		creates = append(creates, tx)
	}
	if len(dropCreates) > 0 {
		b.createPool.Remove(dropCreates...)
	}

	remaining := max - len(creates)
	selected := make([]domain.PaymentTransaction, 0, remaining)
	var drop []string

	for _, tx := range b.mempool.Peek(0) {
		if len(selected) >= remaining {
			break
		}
		if err := ledger.ApplyTransaction(tx, state); err != nil {
			drop = append(drop, tx.ID)
			log.Printf("dropping invalid mempool tx %s: %v", tx.ID, err)
			continue
		}
		selected = append(selected, tx)
	}
	if len(drop) > 0 {
		b.mempool.Remove(drop...)
	}
	return selected, creates
}
