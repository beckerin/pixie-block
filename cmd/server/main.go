package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/beckerin/pixie-block/config"
	"github.com/beckerin/pixie-block/internal/api"
	"github.com/beckerin/pixie-block/internal/chain"
	"github.com/beckerin/pixie-block/internal/consensus/poa"
	"github.com/beckerin/pixie-block/internal/domain"
	"github.com/beckerin/pixie-block/internal/mempool"
	"github.com/beckerin/pixie-block/internal/node"
	"github.com/beckerin/pixie-block/internal/p2p"
	"github.com/beckerin/pixie-block/internal/storage/bolt"
)

type submitAdapter struct {
	bridge *p2p.Bridge
}

func (s *submitAdapter) SubmitTransaction(tx domain.PaymentTransaction) error {
	s.bridge.BroadcastTransaction(tx)
	return nil
}

func (s *submitAdapter) SubmitAccountCreate(tx domain.AccountCreateTransaction) error {
	s.bridge.BroadcastAccountCreate(tx)
	return nil
}

func main() {

	var (
		dataDir      = flag.String("data-dir", "./data", "data directory")
		genesisPath  = flag.String("genesis", "config/genesis.json", "genesis file path")
		keystorePath = flag.String("keystore", "config/keystore.json", "keystore file path")
		validatorKey = flag.String("validator-key", "config/validator-key.json", "validator key file path")
		taxesPath    = flag.String("taxes", "config/taxes.json", "taxes file path")
		apiAddr      = flag.String("api-addr", ":8080", "HTTP API listen address")
		p2pListen    = flag.String("p2p-listen", ":9000", "P2P listen address")
		nodeID       = flag.String("node-id", "node-1", "node identifier")
		peerFlag     = flag.String("peer", "", "peer address (repeatable via multiple flags is not supported; use comma-separated)")
		boltNoSync   = flag.Bool("bolt-nosync", false, "open BoltDB with NoSync (local/demo durability tradeoff; faster block commits)")
	)
	flag.Parse()

	peers := splitPeers(*peerFlag)

	genesis, err := config.LoadGenesis(*genesisPath)
	if err != nil {
		log.Fatalf("load genesis: %v", err)
	}

	keystore, err := config.LoadKeystore(*keystorePath)
	if err != nil {
		log.Fatalf("load keystore: %v", err)
	}
	taxes, err := config.LoadTaxes(*taxesPath)
	if err != nil {
		log.Fatalf("load taxes: %v", err)
	}

	store, err := bolt.Open(*dataDir, log.Default(), *boltNoSync)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	state, err := node.BuildInitialState(genesis, keystore, taxes)
	if err != nil {
		log.Fatalf("build state: %v", err)
	}

	bc, err := chain.New(genesis, store, state, keystore)
	if err != nil {
		log.Fatalf("init chain: %v", err)
	}
	log.Printf("Chain initialized at height %d", bc.Height())

	pool := mempool.New()
	createPool := mempool.NewAccountCreatePool()

	var (
		producer            p2p.BlockProducer
		validatorPrivForAPI string
	)
	if *validatorKey != "" {
		validatorID, validatorKeyB64, err := loadValidatorKey(*validatorKey)
		if err != nil {
			log.Fatalf("load validator key: %v", err)
		}
		validatorPrivForAPI = validatorKeyB64
		p, err := poa.NewProducer(genesis, validatorID, validatorKeyB64)
		if err != nil {
			log.Fatalf("init producer: %v", err)
		}
		producer = p
		log.Printf("Validator producer enabled (%s)", validatorID)
	} else {
		log.Printf("Running as follower (no validator key)")
		// Followers still load validator key for audit views when the file exists.
		if _, priv, err := loadValidatorKey("config/validator-key.json"); err == nil {
			validatorPrivForAPI = priv
		}
	}

	bridge := p2p.NewBridge(genesis.ChainID, *nodeID, bc, pool, createPool, producer, *p2pListen, peers)
	if err := bridge.Start(); err != nil {
		log.Fatalf("start p2p: %v", err)
	} else {
		log.Printf("P2P started successfully")
	}

	adapter := &submitAdapter{bridge: bridge}
	canCreate := producer != nil
	server := api.NewServer(bc, pool, createPool, &keystore, *keystorePath, validatorPrivForAPI, canCreate, adapter, adapter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runBlockProducer(ctx, genesis, pool, createPool, bridge, bc)

	go func() {
		log.Printf("API listening on %s", *apiAddr)
		if err := server.ListenAndServe(ctx, *apiAddr); err != nil {
			log.Printf("api server stopped: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	cancel()
}

func runBlockProducer(ctx context.Context, genesis config.Genesis, pool *mempool.Pool, createPool *mempool.AccountCreatePool, bridge *p2p.Bridge, bc *chain.Blockchain) {
	blockTime := time.Duration(genesis.BlockTimeSeconds) * time.Second
	if blockTime <= 0 {
		blockTime = time.Second
	}
	maxTxs := genesis.MaxTxsPerBlock
	if maxTxs <= 0 {
		maxTxs = 100
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	lastProduce := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n := pool.Len() + createPool.Len()
			if n == 0 {
				continue
			}
			if time.Since(lastProduce) < blockTime && n < maxTxs {
				continue
			}
			if _, produced, err := bridge.ProduceBlockIfReady(); err != nil {
				log.Printf("block production error: %v", err)
			} else if produced {
				lastProduce = time.Now()
				log.Printf("produced block at height %d", bc.Height())
			}
		}
	}
}

func splitPeers(raw string) []string {
	if raw == "" {
		return nil
	}
	var peers []string
	for _, part := range splitComma(raw) {
		if part != "" {
			peers = append(peers, part)
		}
	}
	return peers
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}

type validatorKeyFile struct {
	ValidatorID string `json:"validator_id"`
	PrivateKey  string `json:"private_key"`
}

func loadValidatorKey(path string) (string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	var key validatorKeyFile
	if err := json.Unmarshal(data, &key); err != nil {
		return "", "", err
	}
	if key.ValidatorID == "" || key.PrivateKey == "" {
		return "", "", fmt.Errorf("invalid validator key file")
	}
	return key.ValidatorID, key.PrivateKey, nil
}
