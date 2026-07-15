package config_test

import (
	"testing"

	"github.com/solidk-tech/pixie-block/config"
	"github.com/solidk-tech/pixie-block/internal/domain"
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
