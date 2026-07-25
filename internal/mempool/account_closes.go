package mempool

import (
	"fmt"
	"sync"

	"github.com/beckerin/pixie-block/internal/domain"
)

// AccountClosePool holds pending account close transactions.
type AccountClosePool struct {
	mu    sync.Mutex
	items map[string]domain.AccountCloseTransaction
	order []string
}

func NewAccountClosePool() *AccountClosePool {
	return &AccountClosePool{
		items: make(map[string]domain.AccountCloseTransaction),
		order: make([]string, 0),
	}
}

func (p *AccountClosePool) TryAdd(tx domain.AccountCloseTransaction, check func() error) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.items[tx.ID]; exists {
		return fmt.Errorf("account close %s already in mempool", tx.ID)
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

func (p *AccountClosePool) Remove(ids ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removeLocked(ids...)
}

func (p *AccountClosePool) Commit(ids []string, fn func() error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := fn(); err != nil {
		return err
	}
	p.removeLocked(ids...)
	return nil
}

func (p *AccountClosePool) removeLocked(ids ...string) {
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

func (p *AccountClosePool) Peek(max int) []domain.AccountCloseTransaction {
	p.mu.Lock()
	defer p.mu.Unlock()

	if max <= 0 || max > len(p.order) {
		max = len(p.order)
	}

	out := make([]domain.AccountCloseTransaction, 0, max)
	for i := 0; i < max; i++ {
		out = append(out, p.items[p.order[i]])
	}
	return out
}

func (p *AccountClosePool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.order)
}

func (p *AccountClosePool) Get(id string) (domain.AccountCloseTransaction, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	tx, ok := p.items[id]
	return tx, ok
}
