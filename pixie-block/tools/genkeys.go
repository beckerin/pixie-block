package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	vPub, vPriv, _ := ed25519.GenerateKey(nil)
	mPub, mPriv, _ := ed25519.GenerateKey(nil)
	pPub, pPriv, _ := ed25519.GenerateKey(nil)
	enc := base64.StdEncoding.EncodeToString

	genesis := map[string]any{
		"chain_id":             "pixie-net-1",
		"block_time_seconds":   5,
		"tax_treasury":         "federal_treasury",
		"allowed_tax_accounts": []string{"federal_treasury", "sao_paulo_treasury"},
		"validators": []map[string]string{
			{"id": "validator-1", "public_key": enc(vPub)},
		},
		"accounts": []map[string]any{
			{"id": "federal_treasury", "balance": 0, "currency": "BRL"},
			{"id": "sao_paulo_treasury", "balance": 0, "currency": "BRL"},
			{"id": "merchant_001", "balance": 0, "currency": "BRL"},
			{"id": "person_001", "balance": 10000, "currency": "BRL"},
		},
	}

	keystore := map[string]any{
		"entries": []map[string]string{
			{"account_id": "merchant_001", "private_key": enc(mPriv)},
			{"account_id": "person_001", "private_key": enc(pPriv)},
		},
	}

	g, _ := json.MarshalIndent(genesis, "", "  ")
	k, _ := json.MarshalIndent(keystore, "", "  ")
	if err := os.WriteFile("config/genesis.json", g, 0o644); err != nil {
		panic(err)
	}
	if err := os.WriteFile("config/keystore.json", k, 0o644); err != nil {
		panic(err)
	}
	validatorKey := map[string]string{
		"validator_id": "validator-1",
		"private_key":  enc(vPriv),
	}
	vk, _ := json.MarshalIndent(validatorKey, "", "  ")
	if err := os.WriteFile("config/validator-key.json", vk, 0o600); err != nil {
		panic(err)
	}

	fmt.Println("VALIDATOR_PRIVATE_KEY=" + enc(vPriv))
	fmt.Println("MERCHANT_PUBLIC_KEY=" + enc(mPub))
	fmt.Println("PERSON_PUBLIC_KEY=" + enc(pPub))
}
