package mempool

import (
	"fmt"
	"sync"

	"github.com/solidk-tech/pixie-block/internal/domain"
)

type Pool struct {
	mu    sync.Mutex
	items map[string]domain.PaymentTransaction
	order []string
}

func New() *Pool {
	return &Pool{
		items: make(map[string]domain.PaymentTransaction),
		order: make([]string, 0),
	}
}

func (p *Pool) Add(tx domain.PaymentTransaction) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.items[tx.ID]; exists {
		return fmt.Errorf("transaction %s already in mempool", tx.ID)
	}

	p.items[tx.ID] = tx
	p.order = append(p.order, tx.ID)
	return nil
}

func (p *Pool) Remove(ids ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()

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

func (p *Pool) Get(id string) (domain.PaymentTransaction, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	tx, ok := p.items[id]
	return tx, ok
}
