package config_test

import (
	"os"
	"testing"

	"github.com/beckerin/pixie-block/config"
	"github.com/beckerin/pixie-block/internal/domain"
)

func TestTaxesValidateRejectsDisallowedAccount(t *testing.T) {
	taxes := config.Taxes{
		TaxSplit: map[domain.TaxCode]domain.TaxSplit{
			"ICMS": {RateBPS: 1000, TaxAccount: "other_tax"},
		},
	}
	if err := taxes.Validate([]string{"federal_treasury"}); err == nil {
		t.Fatal("expected validation error for disallowed tax account")
	}
}

func TestTaxesValidateAcceptsAllowedAccount(t *testing.T) {
	taxes := config.Taxes{
		TaxSplit: map[domain.TaxCode]domain.TaxSplit{
			"IBS": {RateBPS: 1700, TaxAccount: "federal_treasury"},
		},
	}
	if err := taxes.Validate([]string{"federal_treasury", "sao_paulo_treasury"}); err != nil {
		t.Fatalf("validate taxes: %v", err)
	}
}

func TestLoadGenesisDefaultsMaxTxsPerBlock(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/genesis.json"
	raw := `{
		"chain_id": "test",
		"tax_treasury": "t",
		"block_time_seconds": 1,
		"validators": [],
		"accounts": []
	}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	genesis, err := config.LoadGenesis(path)
	if err != nil {
		t.Fatal(err)
	}
	if genesis.MaxTxsPerBlock != 100 {
		t.Fatalf("MaxTxsPerBlock = %d, want 100", genesis.MaxTxsPerBlock)
	}
}

func TestLoadGenesisReadsMaxTxsPerBlock(t *testing.T) {
	genesis, err := config.LoadGenesis("genesis.json")
	if err != nil {
		t.Fatal(err)
	}
	if genesis.MaxTxsPerBlock != 1000 {
		t.Fatalf("MaxTxsPerBlock = %d, want 1000", genesis.MaxTxsPerBlock)
	}
	if genesis.BlockTimeSeconds != 1 {
		t.Fatalf("BlockTimeSeconds = %d, want 1", genesis.BlockTimeSeconds)
	}
}
