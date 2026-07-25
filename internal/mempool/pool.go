package mempool

import (
	"fmt"
	"sync"

	"github.com/beckerin/pixie-block/internal/domain"
)

type Pool struct {
	mu       sync.Mutex
	items    map[string]domain.PaymentTransaction
	order    []string
	reserved map[domain.AccountID]int64 // pending gross spends per payer
}

func New() *Pool {
	return &Pool{
		items:    make(map[string]domain.PaymentTransaction),
		order:    make([]string, 0),
		reserved: make(map[domain.AccountID]int64),
	}
}

// TryAdd validates under the pool lock, then inserts and updates reserved.
// check receives the payer's already-reserved gross (excluding tx itself).
func (p *Pool) TryAdd(tx domain.PaymentTransaction, check func(reserved int64) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.items[tx.ID]; exists {
		return fmt.Errorf("transaction %s already in mempool", tx.ID)
	}

	if check != nil {
		if err := check(p.reserved[tx.Payer.ID]); err != nil {
			return err
		}
	}

	p.items[tx.ID] = tx
	p.order = append(p.order, tx.ID)
	p.reserved[tx.Payer.ID] += tx.GrossAmount()
	return nil
}

func (p *Pool) Add(tx domain.PaymentTransaction) error {
	return p.TryAdd(tx, nil)
}

func (p *Pool) Remove(ids ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removeLocked(ids...)
}

// Commit applies fn while holding the pool lock, then releases reserved for ids.
// This closes the race where AppendBlock lowers balances before reserved is cleared,
// which made ValidateAdmit briefly see available = balance-reserved < 0.
func (p *Pool) Commit(ids []string, fn func() error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := fn(); err != nil {
		return err
	}
	p.removeLocked(ids...)
	return nil
}

func (p *Pool) removeLocked(ids ...string) {
	for _, id := range ids {
		tx, ok := p.items[id]
		if !ok {
			continue
		}
		p.releaseLocked(tx)
		delete(p.items, id)
	}

	newOrder := make([]string, 0, len(p.order))
	for _, id := range p.order {
		if _, ok := p.items[id]; ok {
			newOrder = append(newOrder, id)
		}
	}
	p.order = newOrder
}

func (p *Pool) releaseLocked(tx domain.PaymentTransaction) {
	payer := tx.Payer.ID
	p.reserved[payer] -= tx.GrossAmount()
	if p.reserved[payer] <= 0 {
		delete(p.reserved, payer)
	}
}

func (p *Pool) Peek(max int) []domain.PaymentTransaction {
	p.mu.Lock()
	defer p.mu.Unlock()

	if max <= 0 || max > len(p.order) {
		max = len(p.order)
	}

	out := make([]domain.PaymentTransaction, 0, max)
	for i := 0; i < max; i++ {
		out = append(out, p.items[p.order[i]])
	}
	return out
}

func (p *Pool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.order)
}

// Reserved returns pending gross reserved for a payer.
func (p *Pool) Reserved(payer domain.AccountID) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reserved[payer]
}

func (p *Pool) Get(id string) (domain.PaymentTransaction, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	tx, ok := p.items[id]
	return tx, ok
}
