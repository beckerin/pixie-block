package ledger_test

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/beckerin/pixie-block/config"
	"github.com/beckerin/pixie-block/internal/crypto"
	"github.com/beckerin/pixie-block/internal/domain"
	"github.com/beckerin/pixie-block/internal/ledger"
)

var (
	merchantPriv ed25519.PrivateKey
	merchantPub  ed25519.PublicKey
)

func init() {
	merchantPub, merchantPriv, _ = ed25519.GenerateKey(nil)
}

func TestCloneDoesNotShareTaxSplitMap(t *testing.T) {
	state := newTestState(t)
	clone := state.Clone()

	clone.TaxSplit["ICMS"] = domain.TaxSplit{RateBPS: 1, TaxAccount: "tax_treasury"}
	if state.TaxSplit["ICMS"].RateBPS == 1 {
		t.Fatal("Clone shared TaxSplit map with parent")
	}
}

func TestApplyTransactionWithTaxAndDiscount(t *testing.T) {
	state := newTestState(t)

	tx := signTx(domain.PaymentTransaction{
		ID:        "tx-1",
		Timestamp: time.Now().UTC(),
		Payer:     domain.Account{ID: "merchant_001", Type: domain.AccountTypeMerchant, Balance: 0, Currency: "BRL"},
		Payee:     domain.Account{ID: "supplier_042", Type: domain.AccountTypePerson, Balance: 0, Currency: "BRL"},
		Currency:  "BRL",
		Items: []domain.LineItem{{Description: "Serviço", Amount: 100000,
			TaxCodes:  []domain.TaxCode{"ICMS"},
			Discounts: []domain.Discount{{Code: "PIS_CREDIT", Amount: 1650, TaxAccount: "tax_treasury"}}}},
	})

	if err := ledger.ApplyTransaction(tx, state); err != nil {
		t.Fatalf("apply tx: %v", err)
	}

	assertBalance(t, state, "merchant_001", 900000)
	assertBalance(t, state, "supplier_042", 88350)
	assertBalance(t, state, "tax_treasury", 11650)
}

func TestRejectNegativeNetToPayee(t *testing.T) {
	state := newTestState(t)

	tx := signTx(domain.PaymentTransaction{
		ID:        "tx-bad",
		Timestamp: time.Now().UTC(),
		Payer:     domain.Account{ID: "merchant_001", Type: domain.AccountTypeMerchant, Balance: 0, Currency: "BRL"},
		Payee:     domain.Account{ID: "supplier_042", Type: domain.AccountTypePerson, Balance: 0, Currency: "BRL"},
		Currency:  "BRL",
		Items: []domain.LineItem{{Description: "Serviço", Amount: 10000,
			TaxCodes:  []domain.TaxCode{"ICMS"},
			Discounts: []domain.Discount{{Code: "PIS", Amount: 9500, TaxAccount: "tax_treasury"}}}},
	})

	if err := ledger.ApplyTransaction(tx, state); err == nil {
		t.Fatal("expected negative net to payee error")
	}
}

func TestRejectUnauthorizedTaxAccount(t *testing.T) {
	state := newTestState(t)

	tx := signTx(domain.PaymentTransaction{
		ID:        "tx-tax",
		Timestamp: time.Now().UTC(),
		Payer:     domain.Account{ID: "merchant_001", Type: domain.AccountTypeMerchant, Balance: 0, Currency: "BRL"},
		Payee:     domain.Account{ID: "supplier_042", Type: domain.AccountTypePerson, Balance: 0, Currency: "BRL"},
		Currency:  "BRL",
		Items: []domain.LineItem{{Description: "Serviço", Amount: 1000,
			Discounts: []domain.Discount{{Code: "BAD", Amount: 100, TaxAccount: "other_tax"}}}},
	})

	if err := ledger.ApplyTransaction(tx, state); err == nil {
		t.Fatal("expected unauthorized tax account error")
	}
}

func TestRejectInsufficientBalance(t *testing.T) {
	state := newTestState(t)

	tx := signTx(domain.PaymentTransaction{
		ID:        "tx-broke",
		Timestamp: time.Now().UTC(),
		Payer:     domain.Account{ID: "merchant_001", Type: domain.AccountTypeMerchant, Balance: 0, Currency: "BRL"},
		Payee:     domain.Account{ID: "supplier_042", Type: domain.AccountTypePerson, Balance: 0, Currency: "BRL"},
		Currency:  "BRL",
		Items: []domain.LineItem{{Description: "Grande", Amount: 2000000,
			TaxCodes: []domain.TaxCode{"ICMS"},
		}},
	})

	if err := ledger.ApplyTransaction(tx, state); err == nil {
		t.Fatal("expected insufficient balance error")
	}
}

func newTestState(t *testing.T) *ledger.State {
	t.Helper()

	state := ledger.NewState("tax_treasury", []domain.AccountID{"tax_treasury"}, config.Taxes{
		TaxSplit: map[domain.TaxCode]domain.TaxSplit{
			"ICMS": {
				RateBPS:    1000,
				TaxAccount: "tax_treasury",
			},
		},
	})
	state.SetBalance("tax_treasury", 0, "BRL")
	state.SetBalance("merchant_001", 1000000, "BRL")
	state.SetBalance("supplier_042", 0, "BRL")
	state.SetAccountPubKey("merchant_001", merchantPub)
	return state
}

func signTx(tx domain.PaymentTransaction) domain.PaymentTransaction {
	signBytes, _ := crypto.TransactionSignBytes(tx)
	tx.Signature = crypto.Sign(merchantPriv, signBytes)
	return tx
}

func assertBalance(t *testing.T, state *ledger.State, id string, want int64) {
	t.Helper()
	got, ok := state.Balance(domain.AccountID(id))
	if !ok {
		t.Fatalf("account %s not found", id)
	}
	if got != want {
		t.Fatalf("account %s balance: got %d want %d", id, got, want)
	}
}
