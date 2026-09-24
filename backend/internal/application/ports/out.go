package ports

import (
	"context"
	"errors"
	"time"

	"leancore-fintech/backend/internal/domain"
)

var (
	// ErrNotFound: el crédito no existe.
	ErrNotFound = errors.New("credit line not found")
	// ErrDuplicateCreationKey: ya existe un crédito creado con esa llave.
	ErrDuplicateCreationKey = errors.New("duplicate creation idempotency key")
	// ErrDuplicateAccountNumber: el número de cuenta generado ya existe.
	ErrDuplicateAccountNumber = errors.New("duplicate account number")
	// ErrIdempotencyKeyRequired: el comando no trae llave de idempotencia.
	ErrIdempotencyKeyRequired = errors.New("idempotency key required")
	// ErrIdempotencyKeyReused: la llave ya se usó con otro contenido.
	ErrIdempotencyKeyReused = errors.New("idempotency key reused with different payload")
)

// CreatedCredit es un crédito junto con su desembolso y el hash del comando
// que lo creó.
type CreatedCredit struct {
	Credit      *domain.CreditLine
	Entry       domain.Entry
	RequestHash string
}

// CreditLineRepository persiste créditos y movimientos.
type CreditLineRepository interface {
	// Create inserta el crédito y su desembolso de forma atómica.
	Create(ctx context.Context, cl *domain.CreditLine, e domain.Entry, key, hash string) error
	// FindByCreationKey devuelve el crédito creado con esa llave, o ErrNotFound.
	FindByCreationKey(ctx context.Context, key string) (CreatedCredit, error)
	// WithLock ejecuta fn en una transacción con la fila del saldo bloqueada
	// (SELECT ... FOR UPDATE). Si fn devuelve error, nada se persiste.
	WithLock(ctx context.Context, id string, fn func(tx LockedCreditLine) error) error
	Get(ctx context.Context, id string) (*domain.CreditLine, error)
	List(ctx context.Context) ([]*domain.CreditLine, error)
	// ListEntries devuelve los movimientos en orden ascendente de seq, con
	// las asignaciones de cada pago.
	ListEntries(ctx context.Context, id string) ([]domain.Entry, error)
}

// LockedCreditLine es la vista transaccional de un crédito bloqueado.
type LockedCreditLine interface {
	Line() *domain.CreditLine
	// FindEntryByKey devuelve el movimiento con esa llave, o nil.
	FindEntryByKey(ctx context.Context, key string) (*domain.Entry, error)
	// OpenUses devuelve los usos con capital pendiente.
	OpenUses(ctx context.Context) ([]domain.OpenUse, error)
	// Save persiste el nuevo saldo y los movimientos nuevos.
	Save(ctx context.Context, cl *domain.CreditLine, entries []domain.Entry) error
}

// Clock da la fecha actual (para proyecciones "a hoy").
type Clock interface {
	Today() time.Time
}

// IDGenerator genera identificadores y números de cuenta.
type IDGenerator interface {
	NewID() string
	NewAccountNumber() string
}
