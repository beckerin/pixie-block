package chain

import (
	"fmt"
	"sync"
	"time"

	"github.com/beckerin/pixie-block/config"
	"github.com/beckerin/pixie-block/internal/crypto"
	"github.com/beckerin/pixie-block/internal/domain"
	"github.com/beckerin/pixie-block/internal/ledger"
	"github.com/beckerin/pixie-block/internal/node"
	"github.com/beckerin/pixie-block/internal/storage/bolt"
)

type Blockchain struct {
	mu      sync.RWMutex
	genesis config.Genesis
	store   *bolt.Store
	state   *ledger.State
	blocks  []domain.Block
	txIndex map[string]domain.PaymentTransaction
}

func New(genesis config.Genesis, store *bolt.Store, state *ledger.State, keystore config.Keystore) (*Blockchain, error) {
	bc := &Blockchain{
		genesis: genesis,
		store:   store,
		state:   state,
		txIndex: make(map[string]domain.PaymentTransaction),
	}

	if err := node.HydrateState(bc.state, genesis, keystore); err != nil {
		return nil, err
	}

	existing, err := store.LoadBlocks()
	if err != nil {
		return nil, err
	}

	if len(existing) == 0 {
		genesisBlock, err := buildGenesisBlock(genesis)
		if err != nil {
			return nil, err
		}
		if err := store.SaveBlock(genesisBlock); err != nil {
			return nil, err
		}
		if err := store.SaveState(state); err != nil {
			return nil, err
		}
		bc.blocks = []domain.Block{genesisBlock}
		return bc, nil
	}

	bc.blocks = existing
	for _, block := range existing {
		for _, tx := range block.Transactions {
			bc.txIndex[tx.ID] = tx
		}
	}

	savedState, err := store.LoadState()
	if err != nil {
		return nil, err
	}
	if savedState != nil {
		bc.state = savedState
		if err := node.HydrateState(bc.state, genesis, keystore); err != nil {
			return nil, err
		}
	}

	if err := bc.validateChain(keystore); err != nil {
		return nil, err
	}

	return bc, nil
}

func buildGenesisBlock(genesis config.Genesis) (domain.Block, error) {
	block := domain.Block{
		Height:       0,
		Timestamp:    time.Now().UTC(),
		Transactions: nil,
		PreviousHash: nil,
		Validator:    genesis.Validators[0].ID,
	}

	hash, err := crypto.BlockHash(block)
	if err != nil {
		return domain.Block{}, err
	}
	block.Hash = hash
	return block, nil
}

func (bc *Blockchain) validateChain(keystore config.Keystore) error {
	if len(bc.blocks) == 0 {
		return fmt.Errorf("empty chain")
	}

	state := ledger.NewState(
		domain.AccountID(bc.genesis.TaxTreasury),
		toAccountIDs(bc.genesis.AllowedTaxAccounts),
		config.Taxes{TaxSplit: bc.state.TaxSplit},
	)

	for _, acct := range bc.genesis.Accounts {
		state.SetBalance(domain.AccountID(acct.ID), acct.Balance, acct.Currency)
	}

	for _, v := range bc.genesis.Validators {
		pub, err := crypto.ParsePublicKey(v.PublicKey)
		if err != nil {
			return err
		}
		state.AddValidator(v.ID, pub)
	}

	if err := node.HydrateState(state, bc.genesis, keystore); err != nil {
		return err
	}

	prev := bc.blocks[0]
	for i := 1; i < len(bc.blocks); i++ {
		block := bc.blocks[i]
		next, err := ledger.ValidateBlock(block, prev, state)
		if err != nil {
			return fmt.Errorf("block %d: %w", block.Height, err)
		}
		state = next
		prev = block
	}

	return nil
}

func toAccountIDs(ids []string) []domain.AccountID {
	out := make([]domain.AccountID, len(ids))
	for i, id := range ids {
		out[i] = domain.AccountID(id)
	}
	return out
}

func (bc *Blockchain) Height() int64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if len(bc.blocks) == 0 {
		return -1
	}
	return bc.blocks[len(bc.blocks)-1].Height
}

func (bc *Blockchain) LatestBlock() domain.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.blocks[len(bc.blocks)-1]
}

func (bc *Blockchain) GetBlock(height int64) (domain.Block, bool) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	for _, block := range bc.blocks {
		if block.Height == height {
			return block, true
		}
	}
	return domain.Block{}, false
}

func (bc *Blockchain) GetBlocksFrom(height int64) []domain.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	var out []domain.Block
	for _, block := range bc.blocks {
		if block.Height >= height {
			out = append(out, block)
		}
	}
	return out
}

func (bc *Blockchain) GetTransaction(id string) (domain.PaymentTransaction, bool) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	tx, ok := bc.txIndex[id]
	return tx, ok
}

func (bc *Blockchain) State() *ledger.State {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.state.Clone()
}

func (bc *Blockchain) ChainInfo() domain.ChainInfo {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	validators := make([]string, len(bc.genesis.Validators))
	for i, v := range bc.genesis.Validators {
		validators[i] = v.ID
	}

	return domain.ChainInfo{
		ChainID:    bc.genesis.ChainID,
		Height:     bc.Height(),
		Validators: validators,
	}
}

func (bc *Blockchain) ValidatePending(tx domain.PaymentTransaction) error {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return ledger.ValidateTransaction(tx, bc.state)
}

func (bc *Blockchain) AppendBlock(block domain.Block) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	prev := bc.blocks[len(bc.blocks)-1]
	nextState, err := ledger.ValidateBlock(block, prev, bc.state)
	if err != nil {
		return err
	}

	if err := bc.store.SaveBlock(block); err != nil {
		return err
	}
	if err := bc.store.SaveState(nextState); err != nil {
		return err
	}

	bc.blocks = append(bc.blocks, block)
	bc.state = nextState
	for _, tx := range block.Transactions {
		bc.txIndex[tx.ID] = tx
	}

	return nil
}

func (bc *Blockchain) Genesis() config.Genesis {
	return bc.genesis
}
