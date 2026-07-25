package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/beckerin/pixie-block/internal/domain"
)

type account struct {
	ID      string             `json:"id"`
	Type    domain.AccountType `json:"type"`
	Balance int64              `json:"balance"`
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

	allowedTax := make([]string, 0, 2)
	keystoreEntries := make([]map[string]string, 0)
	for _, acct := range accounts {
		if isTaxAccount(acct) {
			allowedTax = append(allowedTax, acct.ID)
			continue
		}
		if !needsKeystoreEntry(acct) {
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
		"genesis_time":         "1970-01-01T00:00:00Z",
		"block_time_seconds":   1,
		"max_txs_per_block":    1000,
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

	out := make([]account, 0, len(file.Accounts))
	for i, acct := range file.Accounts {
		normalized, err := normalizeAccount(acct)
		if err != nil {
			return nil, fmt.Errorf("account[%d] %q: %w", i, acct.ID, err)
		}
		out = append(out, normalized)
	}
	return out, nil
}

func normalizeAccount(acct account) (account, error) {
	if acct.ID == "" {
		return acct, fmt.Errorf("id is required")
	}
	if acct.Type == "" {
		inferred, err := inferAccountType(acct.ID)
		if err != nil {
			return acct, err
		}
		acct.Type = inferred
	}
	switch acct.Type {
	case domain.AccountTypePerson, domain.AccountTypeMerchant, domain.AccountTypeTreasury:
		return acct, nil
	default:
		return acct, fmt.Errorf("unknown type %q", acct.Type)
	}
}

func inferAccountType(id string) (domain.AccountType, error) {
	switch {
	case strings.HasPrefix(id, "person_"):
		return domain.AccountTypePerson, nil
	case strings.HasPrefix(id, "merchant_"):
		return domain.AccountTypeMerchant, nil
	case id == "federal_treasury" || strings.HasSuffix(id, "_treasury"):
		return domain.AccountTypeTreasury, nil
	default:
		return "", fmt.Errorf("cannot infer type from id")
	}
}

func isTaxAccount(acct account) bool {
	return acct.Type == domain.AccountTypeTreasury
}

func needsKeystoreEntry(acct account) bool {
	return acct.Type == domain.AccountTypePerson || acct.Type == domain.AccountTypeMerchant
}
