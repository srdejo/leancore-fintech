// Package memory implementa ports.CreditLineRepository en memoria (tests y demos).
package memory

import (
	"context"
	"sync"

	"leancore-fintech/backend/internal/application/ports"
	"leancore-fintech/backend/internal/domain"
)

// Repository es un repositorio en memoria de ports.CreditLineRepository. WithLock
// serializa por crédito y solo confirma lo que Save registró si fn no falla.
type Repository struct {
	mu        sync.Mutex
	locks     map[string]*sync.Mutex
	lines     map[string]*domain.CreditLine
	entries   map[string][]domain.Entry
	byCreate  map[string]string // llave de creación -> id
	hashes    map[string]string // id -> hash de creación
	accounts  map[string]bool
	saveCalls int
}

func NewRepository() *Repository {
	return &Repository{locks: map[string]*sync.Mutex{}, lines: map[string]*domain.CreditLine{},
		entries: map[string][]domain.Entry{}, byCreate: map[string]string{}, hashes: map[string]string{},
		accounts: map[string]bool{}}
}

func (r *Repository) Create(_ context.Context, cl *domain.CreditLine, e domain.Entry, key, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byCreate[key]; ok {
		return ports.ErrDuplicateCreationKey
	}
	if r.accounts[cl.AccountNumber] {
		return ports.ErrDuplicateAccountNumber
	}
	r.lines[cl.ID] = cl.Clone()
	r.entries[cl.ID] = []domain.Entry{e}
	r.byCreate[key], r.hashes[cl.ID], r.accounts[cl.AccountNumber] = cl.ID, hash, true
	r.locks[cl.ID] = &sync.Mutex{}
	return nil
}

func (r *Repository) FindByCreationKey(_ context.Context, key string) (ports.CreatedCredit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byCreate[key]
	if !ok {
		return ports.CreatedCredit{}, ports.ErrNotFound
	}
	return ports.CreatedCredit{Credit: r.lines[id].Clone(), Entry: r.entries[id][0], RequestHash: r.hashes[id]}, nil
}

type lockedTx struct {
	r       *Repository
	id      string
	line    *domain.CreditLine
	pending []domain.Entry
	saved   *domain.CreditLine
}

func (t *lockedTx) Line() *domain.CreditLine { return t.line }

func (t *lockedTx) FindEntryByKey(_ context.Context, key string) (*domain.Entry, error) {
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	for _, e := range t.r.entries[t.id] {
		if e.IdempotencyKey == key {
			return &e, nil
		}
	}
	return nil, nil
}

func (t *lockedTx) OpenUses(_ context.Context) ([]domain.OpenUse, error) {
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	pending := map[string]domain.Money{}
	var uses []domain.OpenUse
	for _, e := range t.r.entries[t.id] {
		if e.Type == domain.EntryDisbursement || e.Type == domain.EntryPurchase {
			pending[e.ID] = e.Amount
		}
		for _, a := range e.Allocations {
			pending[a.UseEntryID] -= a.ToCapital
		}
	}
	for _, e := range t.r.entries[t.id] {
		if p, ok := pending[e.ID]; ok && p > 0 {
			uses = append(uses, domain.OpenUse{EntryID: e.ID, Seq: e.Seq, Pending: p})
		}
	}
	return uses, nil
}

func (t *lockedTx) Save(_ context.Context, cl *domain.CreditLine, entries []domain.Entry) error {
	t.saved = cl.Clone()
	t.pending = append(t.pending, entries...)
	return nil
}

func (r *Repository) WithLock(ctx context.Context, id string, fn func(tx ports.LockedCreditLine) error) error {
	r.mu.Lock()
	l, ok := r.locks[id]
	r.mu.Unlock()
	if !ok {
		return ports.ErrNotFound
	}
	l.Lock()
	defer l.Unlock()

	r.mu.Lock()
	tx := &lockedTx{r: r, id: id, line: r.lines[id].Clone()}
	r.mu.Unlock()
	if err := fn(tx); err != nil {
		return err // rollback: nada de lo registrado se confirma
	}
	if tx.saved != nil {
		r.mu.Lock()
		r.lines[id] = tx.saved
		r.entries[id] = append(r.entries[id], tx.pending...)
		r.saveCalls++
		r.mu.Unlock()
	}
	return nil
}

func (r *Repository) Get(_ context.Context, id string) (*domain.CreditLine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cl, ok := r.lines[id]
	if !ok {
		return nil, ports.ErrNotFound
	}
	return cl.Clone(), nil
}

func (r *Repository) List(_ context.Context) ([]*domain.CreditLine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.CreditLine
	for _, cl := range r.lines {
		out = append(out, cl.Clone())
	}
	return out, nil
}

func (r *Repository) ListEntries(_ context.Context, id string) ([]domain.Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.Entry(nil), r.entries[id]...), nil
}

func (r *Repository) EntryCount(id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries[id])
}

// SaveCalls cuenta las transacciones confirmadas con escritura.
func (r *Repository) SaveCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saveCalls
}

// Len es la cantidad de créditos.
func (r *Repository) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.lines)
}
