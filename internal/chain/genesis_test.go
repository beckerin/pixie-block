package chain_test

import (
	"log"
	"path/filepath"
	"testing"

	"github.com/beckerin/pixie-block/config"
	"github.com/beckerin/pixie-block/internal/chain"
	"github.com/beckerin/pixie-block/internal/node"
	"github.com/beckerin/pixie-block/internal/storage/bolt"
)

func TestGenesisHashIsDeterministic(t *testing.T) {
	genesis, err := config.LoadGenesis("../../config/genesis.json")
	if err != nil {
		t.Fatalf("load genesis: %v", err)
	}
	keystore, err := config.LoadKeystore("../../config/keystore.json")
	if err != nil {
		t.Fatalf("load keystore: %v", err)
	}
	taxes, err := config.LoadTaxes("../../config/taxes.json")
	if err != nil {
		t.Fatalf("load taxes: %v", err)
	}

	hash1 := genesisHash(t, t.TempDir(), genesis, keystore, taxes)
	hash2 := genesisHash(t, t.TempDir(), genesis, keystore, taxes)

	if string(hash1) != string(hash2) {
		t.Fatalf("genesis hashes differ across nodes:\n  %x\n  %x", hash1, hash2)
	}
}

func genesisHash(t *testing.T, dir string, genesis config.Genesis, keystore config.Keystore, taxes config.Taxes) []byte {
	t.Helper()
	store, err := bolt.Open(filepath.Join(dir, "data"), log.Default(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	state, err := node.BuildInitialState(genesis, keystore, taxes)
	if err != nil {
		t.Fatal(err)
	}
	bc, err := chain.New(genesis, store, state, keystore)
	if err != nil {
		t.Fatal(err)
	}
	return bc.LatestBlock().Hash
}
