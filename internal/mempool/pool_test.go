package mempool_test

import (
	"fmt"
	"testing"

	"github.com/beckerin/pixie-block/internal/domain"
	"github.com/beckerin/pixie-block/internal/mempool"
)

func TestTryAddRejectsWhenCheckFails(t *testing.T) {
	pool := mempool.New()
	tx := domain.PaymentTransaction{ID: "tx-1", Payer: domain.Account{ID: "a"}}

	err := pool.TryAdd(tx, func(int64) error {
		return fmt.Errorf("insufficient balance: have 0 need 100")
	})
	if err == nil {
		t.Fatal("expected check failure")
	}
	if pool.Len() != 0 {
		t.Fatalf("len = %d, want 0", pool.Len())
	}
	if pool.Reserved("a") != 0 {
		t.Fatalf("reserved = %d, want 0", pool.Reserved("a"))
	}
}

func TestReservedTracksGrossAndReleasesOnRemove(t *testing.T) {
	pool := mempool.New()
	first := domain.PaymentTransaction{
		ID:    "tx-1",
		Payer: domain.Account{ID: "payer"},
		Items: []domain.LineItem{{Amount: 700}},
	}
	if err := pool.Add(first); err != nil {
		t.Fatal(err)
	}
	if got := pool.Reserved("payer"); got != 700 {
		t.Fatalf("reserved = %d, want 700", got)
	}

	second := domain.PaymentTransaction{
		ID:    "tx-2",
		Payer: domain.Account{ID: "payer"},
		Items: []domain.LineItem{{Amount: 700}},
	}

	err := pool.TryAdd(second, func(reserved int64) error {
		const balance = 1000
		if reserved+second.GrossAmount() > balance {
			return fmt.Errorf("insufficient balance: have %d need %d", balance-reserved, second.GrossAmount())
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected overspend rejection")
	}
	if pool.Len() != 1 {
		t.Fatalf("len = %d, want 1", pool.Len())
	}
	if got := pool.Reserved("payer"); got != 700 {
		t.Fatalf("reserved after reject = %d, want 700", got)
	}

	pool.Remove("tx-1")
	if pool.Len() != 0 {
		t.Fatalf("len = %d, want 0", pool.Len())
	}
	if got := pool.Reserved("payer"); got != 0 {
		t.Fatalf("reserved after remove = %d, want 0", got)
	}
}

func TestTryAddSeesReservedWithoutPendingList(t *testing.T) {
	pool := mempool.New()
	first := domain.PaymentTransaction{
		ID:    "tx-1",
		Payer: domain.Account{ID: "payer"},
		Items: []domain.LineItem{{Amount: 400}},
	}
	second := domain.PaymentTransaction{
		ID:    "tx-2",
		Payer: domain.Account{ID: "payer"},
		Items: []domain.LineItem{{Amount: 400}},
	}
	if err := pool.Add(first); err != nil {
		t.Fatal(err)
	}
	if err := pool.TryAdd(second, func(reserved int64) error {
		if reserved != 400 {
			return fmt.Errorf("reserved = %d, want 400", reserved)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := pool.Reserved("payer"); got != 800 {
		t.Fatalf("reserved = %d, want 800", got)
	}
}

func TestCommitReleasesReservedAfterFn(t *testing.T) {
	pool := mempool.New()
	tx := domain.PaymentTransaction{
		ID:    "tx-1",
		Payer: domain.Account{ID: "payer"},
		Items: []domain.LineItem{{Amount: 500}},
	}
	if err := pool.Add(tx); err != nil {
		t.Fatal(err)
	}

	called := false
	if err := pool.Commit([]string{"tx-1"}, func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("commit fn was not called")
	}
	if got := pool.Reserved("payer"); got != 0 {
		t.Fatalf("reserved after commit = %d, want 0", got)
	}
	if pool.Len() != 0 {
		t.Fatalf("len after commit = %d, want 0", pool.Len())
	}
}

func TestCommitPropagatesFnErrorWithoutRelease(t *testing.T) {
	pool := mempool.New()
	tx := domain.PaymentTransaction{
		ID:    "tx-1",
		Payer: domain.Account{ID: "payer"},
		Items: []domain.LineItem{{Amount: 500}},
	}
	if err := pool.Add(tx); err != nil {
		t.Fatal(err)
	}
	err := pool.Commit([]string{"tx-1"}, func() error {
		return fmt.Errorf("append failed")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := pool.Reserved("payer"); got != 500 {
		t.Fatalf("reserved after failed commit = %d, want 500", got)
	}
	if pool.Len() != 1 {
		t.Fatalf("len after failed commit = %d, want 1", pool.Len())
	}
}
