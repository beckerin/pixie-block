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
	mu          sync.RWMutex
	genesis     config.Genesis
	store       *bolt.Store
	state       *ledger.State
	blocks      []domain.Block
	txIndex     map[string]domain.PaymentTransaction
	createIndex map[string]domain.AccountCreateTransaction
	closeIndex  map[string]domain.AccountCloseTransaction
}

func New(genesis config.Genesis, store *bolt.Store, state *ledger.State, keystore config.Keystore) (*Blockchain, error) {
	bc := &Blockchain{
		genesis:     genesis,
		store:       store,
		state:       state,
		txIndex:     make(map[string]domain.PaymentTransaction),
		createIndex: make(map[string]domain.AccountCreateTransaction),
		closeIndex:  make(map[string]domain.AccountCloseTransaction),
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
		for _, create := range block.AccountCreates {
			bc.createIndex[create.ID] = create
		}
		for _, closeTx := range block.AccountCloses {
			bc.closeIndex[closeTx.ID] = closeTx
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

	// Rehydrate pubkeys from on-chain account creates (followers may lack keystore entries).
	if err := hydrateAccountCreatePubKeys(bc.state, existing); err != nil {
		return nil, err
	}

	if err := bc.validateChain(keystore); err != nil {
		return nil, err
	}

	return bc, nil
}

func hydrateAccountCreatePubKeys(state *ledger.State, blocks []domain.Block) error {
	for _, block := range blocks {
		for _, create := range block.AccountCreates {
			pub, err := crypto.ParsePublicKey(create.PublicKey)
			if err != nil {
				return fmt.Errorf("account create %s pubkey: %w", create.ID, err)
			}
			state.SetAccountPubKey(string(create.Account.ID), pub)
		}
		for _, closeTx := range block.AccountCloses {
			state.RemoveAccountPubKey(string(closeTx.AccountID))
		}
	}
	return nil
}

func buildGenesisBlock(genesis config.Genesis) (domain.Block, error) {
	// Fixed timestamp so every node derives the same genesis hash.
	ts := genesis.GenesisTime
	if ts.IsZero() {
		ts = time.Unix(0, 0).UTC()
	}
	block := domain.Block{
		Height:       0,
		Timestamp:    ts,
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
		toAccountIDs(bc.genesis.AllowedTaxAccounts),
		config.Taxes{TaxSplit: bc.state.TaxSplit},
	)

	for _, acct := range bc.genesis.Accounts {
		state.SetAccount(domain.Account{
			ID:      domain.AccountID(acct.ID),
			Type:    acct.Type,
			Balance: acct.Balance,
		})
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

func (bc *Blockchain) GetAccountCreate(id string) (domain.AccountCreateTransaction, bool) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	tx, ok := bc.createIndex[id]
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

	height := int64(-1)
	if len(bc.blocks) > 0 {
		height = bc.blocks[len(bc.blocks)-1].Height
	}

	return domain.ChainInfo{
		ChainID:    bc.genesis.ChainID,
		Height:     height,
		Validators: validators,
	}
}

func (bc *Blockchain) ValidatePending(tx domain.PaymentTransaction) error {
	return bc.ValidateAdmit(tx, 0)
}

// ValidateAdmit checks tx against confirmed state after subtracting reservedGross
// already held in the mempool for the payer (O(1) vs reapplying pending).
func (bc *Blockchain) ValidateAdmit(tx domain.PaymentTransaction, reservedGross int64) error {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	state := bc.state.Clone()
	if acct, ok := state.Accounts[tx.Payer.ID]; ok {
		acct.Balance -= reservedGross
		state.Accounts[tx.Payer.ID] = acct
	}
	return ledger.ValidateTransaction(tx, state)
}

func (bc *Blockchain) ValidateAccountCreate(tx domain.AccountCreateTransaction) error {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return ledger.ValidateAccountCreate(tx, bc.state.Clone())
}

func (bc *Blockchain) ValidateAccountClose(tx domain.AccountCloseTransaction) error {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return ledger.ValidateAccountClose(tx, bc.state.Clone())
}

// ValidatePendingWith checks tx against confirmed state after applying pending spends in order.
// Prefer ValidateAdmit for the hot admit path.
func (bc *Blockchain) ValidatePendingWith(tx domain.PaymentTransaction, pending []domain.PaymentTransaction) error {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	state := bc.state.Clone()
	for _, p := range pending {
		if err := ledger.ApplyTransaction(p, state); err != nil {
			continue
		}
	}
	return ledger.ValidateTransaction(tx, state)
}

func (bc *Blockchain) AppendBlock(block domain.Block) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	prev := bc.blocks[len(bc.blocks)-1]
	nextState, err := ledger.ValidateBlock(block, prev, bc.state)
	if err != nil {
		return err
	}

	if err := bc.store.SaveBlockAndState(block, nextState); err != nil {
		return err
	}

	bc.blocks = append(bc.blocks, block)
	bc.state = nextState
	for _, tx := range block.Transactions {
		bc.txIndex[tx.ID] = tx
	}
	for _, create := range block.AccountCreates {
		bc.createIndex[create.ID] = create
	}
	for _, closeTx := range block.AccountCloses {
		bc.closeIndex[closeTx.ID] = closeTx
	}

	return nil
}

func (bc *Blockchain) Genesis() config.Genesis {
	return bc.genesis
}
