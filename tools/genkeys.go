package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type account struct {
	ID       string `json:"id"`
	Balance  int64  `json:"balance"`
	Currency string `json:"currency"`
}

type accountsFile struct {
	Accounts []account `json:"accounts"`
}

func main() {
	accounts, err := loadAccounts("tools/accounts.json")
	if err != nil {
		panic(err)
	}

	vPub, vPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(fmt.Errorf("generate validator key: %w", err))
	}
	enc := base64.StdEncoding.EncodeToString

	allowedTax := make([]string, 0, 28)
	keystoreEntries := make([]map[string]string, 0)
	for _, acct := range accounts {
		if isTaxAccount(acct.ID) {
			allowedTax = append(allowedTax, acct.ID)
			continue
		}
		if !needsKeystoreEntry(acct.ID) {
			continue
		}
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			panic(fmt.Errorf("generate key for %s: %w", acct.ID, err))
		}
		keystoreEntries = append(keystoreEntries, map[string]string{
			"account_id":  acct.ID,
			"private_key": enc(priv),
		})
	}

	genesis := map[string]any{
		"chain_id":             "pixie-net-1",
		"block_time_seconds":   5,
		"tax_treasury":         "federal_treasury",
		"allowed_tax_accounts": allowedTax,
		"validators": []map[string]string{
			{"id": "validator-1", "public_key": enc(vPub)},
		},
		"accounts": accounts,
	}

	keystore := map[string]any{
		"entries": keystoreEntries,
	}

	g, err := json.MarshalIndent(genesis, "", "  ")
	if err != nil {
		panic(fmt.Errorf("marshal genesis: %w", err))
	}
	k, err := json.MarshalIndent(keystore, "", "  ")
	if err != nil {
		panic(fmt.Errorf("marshal keystore: %w", err))
	}
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
	vk, err := json.MarshalIndent(validatorKey, "", "  ")
	if err != nil {
		panic(fmt.Errorf("marshal validator key: %w", err))
	}
	if err := os.WriteFile("config/validator-key.json", vk, 0o600); err != nil {
		panic(err)
	}

	fmt.Printf("accounts=%d allowed_tax=%d keystore=%d\n",
		len(accounts), len(allowedTax), len(keystoreEntries))
	fmt.Println("VALIDATOR_PRIVATE_KEY=" + enc(vPriv))
}

func loadAccounts(path string) ([]account, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read accounts: %w", err)
	}
	var file accountsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse accounts: %w", err)
	}
	if len(file.Accounts) == 0 {
		return nil, fmt.Errorf("accounts file has no accounts")
	}
	return file.Accounts, nil
}

func isTaxAccount(id string) bool {
	return id == "federal_treasury" || strings.HasSuffix(id, "_treasury")
}

func needsKeystoreEntry(id string) bool {
	return strings.HasPrefix(id, "person_") || strings.HasPrefix(id, "merchant_")
}
