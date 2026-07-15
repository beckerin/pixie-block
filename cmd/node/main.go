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

	"github.com/solidk-tech/pixie-block/config"
	"github.com/solidk-tech/pixie-block/internal/api"
	"github.com/solidk-tech/pixie-block/internal/chain"
	"github.com/solidk-tech/pixie-block/internal/consensus/poa"
	"github.com/solidk-tech/pixie-block/internal/domain"
	"github.com/solidk-tech/pixie-block/internal/mempool"
	"github.com/solidk-tech/pixie-block/internal/node"
	"github.com/solidk-tech/pixie-block/internal/p2p"
	"github.com/solidk-tech/pixie-block/internal/storage/bolt"
)

type submitAdapter struct {
	bridge *p2p.Bridge
}

func (s *submitAdapter) SubmitTransaction(tx domain.PaymentTransaction) error {
	s.bridge.BroadcastTransaction(tx)
	return nil
}

func main() {
	log.Printf("Starting Pixie Node")
	var (
		dataDir      = flag.String("data-dir", "./data", "data directory")
		genesisPath  = flag.String("genesis", "config/genesis.json", "genesis file path")
		keystorePath = flag.String("keystore", "config/keystore.json", "keystore file path")
		validatorKey = flag.String("validator-key", "config/validator-key.json", "validator key file path")
		apiAddr      = flag.String("api-addr", ":8080", "HTTP API listen address")
		p2pListen    = flag.String("p2p-listen", ":9000", "P2P listen address")
		nodeID       = flag.String("node-id", "node-1", "node identifier")
		peerFlag     = flag.String("peer", "", "peer address (repeatable via multiple flags is not supported; use comma-separated)")
	)
	flag.Parse()

	peers := splitPeers(*peerFlag)

	genesis, err := config.LoadGenesis(*genesisPath)
	if err != nil {
		log.Fatalf("load genesis: %v", err)
	} else {
		log.Printf("Genesis file loaded successfully")
	}

	keystore, err := config.LoadKeystore(*keystorePath)
	if err != nil {
		log.Fatalf("load keystore: %v", err)
	} else {
		log.Printf("Keystore file loaded successfully")
	}

	validatorID, validatorKeyB64, err := loadValidatorKey(*validatorKey)
	if err != nil {
		log.Fatalf("load validator key: %v", err)
	} else {
		log.Printf("Validator key file loaded successfully")
	}

	store, err := bolt.Open(*dataDir, log.Default())
	if err != nil {
		log.Fatalf("open store: %v", err)
	} else {
		log.Printf("Store opened successfully")
	}
	defer store.Close()

	
	state, err := node.BuildInitialState(genesis, keystore)
	if err != nil {
		log.Fatalf("build state: %v", err)
	} else {
		log.Printf("Initial state built successfully")
	}

	bc, err := chain.New(genesis, store, state, keystore)
	if err != nil {
		log.Fatalf("init chain: %v", err)
	} else {
		log.Printf("Chain initialized successfully with height %d", bc.Height())
	}

	pool := mempool.New()

	producer, err := poa.NewProducer(genesis, validatorID, validatorKeyB64)
	if err != nil {
		log.Fatalf("init producer: %v", err)
	} else {
		log.Printf("Producer initialized successfully")
	}

	bridge := p2p.NewBridge(genesis.ChainID, *nodeID, bc, pool, producer, *p2pListen, peers)
	if err := bridge.Start(); err != nil {
		log.Fatalf("start p2p: %v", err)
	} else {
		log.Printf("P2P started successfully")
	}

	adapter := &submitAdapter{bridge: bridge}
	server := api.NewServer(bc, pool, keystore, adapter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Printf("Starting block production successfully")
	go func() {
		ticker := time.NewTicker(time.Duration(genesis.BlockTimeSeconds) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, produced, err := bridge.ProduceBlockIfReady(); err != nil {
					log.Printf("block production error: %v", err)
				} else if produced {
					log.Printf("produced block at height %d", bc.Height())
				}
			}
		}
	}()

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
