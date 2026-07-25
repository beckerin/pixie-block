package mempool

import (
	"fmt"
	"sync"

	"github.com/beckerin/pixie-block/internal/domain"
)

// AccountCreatePool holds pending account create transactions.
type AccountCreatePool struct {
	mu    sync.Mutex
	items map[string]domain.AccountCreateTransaction
	order []string
}

func NewAccountCreatePool() *AccountCreatePool {
	return &AccountCreatePool{
		items: make(map[string]domain.AccountCreateTransaction),
		order: make([]string, 0),
	}
}

func (p *AccountCreatePool) TryAdd(tx domain.AccountCreateTransaction, check func() error) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.items[tx.ID]; exists {
		return fmt.Errorf("account create %s already in mempool", tx.ID)
	}
	if check != nil {
		if err := check(); err != nil {
			return err
		}
	}
	p.items[tx.ID] = tx
	p.order = append(p.order, tx.ID)
	return nil
}

func (p *AccountCreatePool) Remove(ids ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removeLocked(ids...)
}

func (p *AccountCreatePool) Commit(ids []string, fn func() error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := fn(); err != nil {
		return err
	}
	p.removeLocked(ids...)
	return nil
}

func (p *AccountCreatePool) removeLocked(ids ...string) {
	for _, id := range ids {
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

func (p *AccountCreatePool) Peek(max int) []domain.AccountCreateTransaction {
	p.mu.Lock()
	defer p.mu.Unlock()

	if max <= 0 || max > len(p.order) {
		max = len(p.order)
	}

	out := make([]domain.AccountCreateTransaction, 0, max)
	for i := 0; i < max; i++ {
		out = append(out, p.items[p.order[i]])
	}
	return out
}

func (p *AccountCreatePool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.order)
}

func (p *AccountCreatePool) Get(id string) (domain.AccountCreateTransaction, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	tx, ok := p.items[id]
	return tx, ok
}
