package bolt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"

	"github.com/solidk-tech/pixie-block/internal/domain"
	"github.com/solidk-tech/pixie-block/internal/ledger"
)

const (
	blocksBucket = "blocks"
	stateBucket  = "state"
	stateKey     = "current"
)

type Store struct {
	db *bolt.DB
}

func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db, err := bolt.Open(filepath.Join(dataDir, "pixie.db"), 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	store := &Store{db: db}
	if err := store.db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(blocksBucket)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(stateBucket)); err != nil {
			return err
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) SaveBlock(block domain.Block) error {
	data, err := json.Marshal(block)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(blocksBucket))
		key := fmt.Sprintf("%020d", block.Height)
		return bucket.Put([]byte(key), data)
	})
}

func (s *Store) LoadBlocks() ([]domain.Block, error) {
	var blocks []domain.Block

	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(blocksBucket))
		return bucket.ForEach(func(k, v []byte) error {
			var block domain.Block
			if err := json.Unmarshal(v, &block); err != nil {
				return err
			}
			blocks = append(blocks, block)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	// sort by height (bucket iteration order is byte-sorted which works with zero-padded keys)
	for i := 0; i < len(blocks); i++ {
		for j := i + 1; j < len(blocks); j++ {
			if blocks[j].Height < blocks[i].Height {
				blocks[i], blocks[j] = blocks[j], blocks[i]
			}
		}
	}

	return blocks, nil
}

type persistedState struct {
	Accounts        map[domain.AccountID]domain.Account `json:"accounts"`
	TaxTreasury     domain.AccountID                    `json:"tax_treasury"`
	AllowedTaxAccts []domain.AccountID                  `json:"allowed_tax_accounts"`
}

func (s *Store) SaveState(state *ledger.State) error {
	allowed := make([]domain.AccountID, 0, len(state.AllowedTaxAccts))
	for id := range state.AllowedTaxAccts {
		allowed = append(allowed, id)
	}

	payload := persistedState{
		Accounts:        state.Accounts,
		TaxTreasury:     state.TaxTreasury,
		AllowedTaxAccts: allowed,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(stateBucket))
		return bucket.Put([]byte(stateKey), data)
	})
}

func (s *Store) LoadState() (*ledger.State, error) {
	var payload persistedState
	var found bool

	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(stateBucket))
		data := bucket.Get([]byte(stateKey))
		if data == nil {
			return nil
		}
		found = true
		return json.Unmarshal(data, &payload)
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	state := ledger.NewState(payload.TaxTreasury, payload.AllowedTaxAccts)
	for id, acct := range payload.Accounts {
		state.Accounts[id] = acct
	}

	return state, nil
}
