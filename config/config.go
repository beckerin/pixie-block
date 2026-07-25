package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/beckerin/pixie-block/internal/domain"
)

type ValidatorConfig struct {
	ID        string `json:"id"`
	PublicKey string `json:"public_key"`
}

type AccountConfig struct {
	ID       string             `json:"id"`
	Type     domain.AccountType `json:"type"`
	Balance  int64              `json:"balance"`
	Currency string             `json:"currency"`
}

type KeystoreEntry struct {
	AccountID  string `json:"account_id"`
	PrivateKey string `json:"private_key"`
}

type Keystore struct {
	Entries []KeystoreEntry `json:"entries"`
}

type Taxes struct {
	TaxSplit map[domain.TaxCode]domain.TaxSplit `json:"tax_splits"`
}

type Genesis struct {
	ChainID            string            `json:"chain_id"`
	BlockTimeSeconds   int               `json:"block_time_seconds"`
	TaxTreasury        string            `json:"tax_treasury"`
	AllowedTaxAccounts []string          `json:"allowed_tax_accounts"`
	Validators         []ValidatorConfig `json:"validators"`
	Accounts           []AccountConfig   `json:"accounts"`
}

type Config struct {
	Genesis         Genesis
	Keystore        Keystore
	DataDir         string
	APIAddr         string
	P2PListen       string
	Peers           []string
	GenesisPath     string
	KeystorePath    string
	ValidatorID     string
	ValidatorKeyB64 string
	NodeID          string
}

func LoadGenesis(path string) (Genesis, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Genesis{}, fmt.Errorf("read genesis: %w", err)
	}
	var genesis Genesis
	if err := json.Unmarshal(data, &genesis); err != nil {
		return Genesis{}, fmt.Errorf("parse genesis: %w", err)
	}
	if genesis.ChainID == "" {
		return Genesis{}, fmt.Errorf("genesis chain_id is required")
	}
	if genesis.TaxTreasury == "" {
		return Genesis{}, fmt.Errorf("genesis tax_treasury is required")
	}
	if len(genesis.AllowedTaxAccounts) == 0 {
		genesis.AllowedTaxAccounts = []string{genesis.TaxTreasury}
	}
	return genesis, nil
}

func LoadKeystore(path string) (Keystore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Keystore{}, fmt.Errorf("read keystore: %w", err)
	}
	var keystore Keystore
	if err := json.Unmarshal(data, &keystore); err != nil {
		return Keystore{}, fmt.Errorf("parse keystore: %w", err)
	}
	return keystore, nil
}

func LoadTaxes(path string) (Taxes, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Taxes{}, fmt.Errorf("read taxes: %w", err)
	}
	var taxes Taxes
	if err := json.Unmarshal(data, &taxes); err != nil {
		return Taxes{}, fmt.Errorf("parse taxes: %w", err)
	}
	return taxes, nil
}

func (t Taxes) Validate(allowedTaxAccounts []string) error {
	allowed := make(map[domain.AccountID]struct{}, len(allowedTaxAccounts))
	for _, id := range allowedTaxAccounts {
		allowed[domain.AccountID(id)] = struct{}{}
	}
	if len(t.TaxSplit) == 0 {
		return fmt.Errorf("tax_splits must not be empty")
	}
	for code, split := range t.TaxSplit {
		if split.RateBPS < 0 {
			return fmt.Errorf("tax code %q: rate_bps cannot be negative", code)
		}
		if split.TaxAccount == "" {
			return fmt.Errorf("tax code %q: tax_account is required", code)
		}
		if _, ok := allowed[split.TaxAccount]; !ok {
			return fmt.Errorf("tax code %q: tax account %q is not in allowed_tax_accounts", code, split.TaxAccount)
		}
	}
	return nil
}

func (k Keystore) PrivateKeyFor(accountID string) (string, bool) {
	for _, entry := range k.Entries {
		if entry.AccountID == accountID {
			return entry.PrivateKey, true
		}
	}
	return "", false
}
