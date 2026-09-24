// Package ports define los puertos de la aplicación: lo que ofrece (in) y lo
// que necesita de la infraestructura (out).
package ports

import (
	"context"
	"time"

	"leancore-fintech/backend/internal/domain"
)

// OpenCreditCommand abre un crédito con su desembolso inicial.
type OpenCreditCommand struct {
	IdempotencyKey   string
	Holder           string
	LimitCents       domain.Money
	RateEABps        int
	AmountCents      domain.Money
	DisbursementDate time.Time
	Description      string
}

// MovementCommand registra un consumo o un pago sobre un crédito.
type MovementCommand struct {
	CreditID       string
	IdempotencyKey string
	AmountCents    domain.Money
	Date           time.Time
	Description    string
}

// LiquidateCommand liquida el interés de un crédito a una fecha.
type LiquidateCommand struct {
	CreditID       string
	IdempotencyKey string
	Date           time.Time
}

// OpenResult es el resultado de abrir un crédito. Replayed indica que se
// devolvió el resultado de un comando ya procesado con la misma llave.
type OpenResult struct {
	Credit   *domain.CreditLine
	Entry    domain.Entry
	Replayed bool
}

// MovementResult es el movimiento principal creado por un comando.
type MovementResult struct {
	Entry    domain.Entry
	Replayed bool
}

// CreditView es el estado de un crédito con la proyección a hoy.
type CreditView struct {
	Credit     *domain.CreditLine
	Projection domain.Preview
}

// EntriesView es el historial, del más reciente al más antiguo, con totales.
type EntriesView struct {
	Entries     []domain.Entry
	TotalDebit  domain.Money
	TotalCredit domain.Money
}

type OpenCredit interface {
	OpenCredit(ctx context.Context, cmd OpenCreditCommand) (OpenResult, error)
}

type RegisterPurchase interface {
	RegisterPurchase(ctx context.Context, cmd MovementCommand) (MovementResult, error)
}

type RegisterPayment interface {
	RegisterPayment(ctx context.Context, cmd MovementCommand) (MovementResult, error)
}

type LiquidateInterest interface {
	LiquidateInterest(ctx context.Context, cmd LiquidateCommand) (MovementResult, error)
}

type GetCredit interface {
	GetCredit(ctx context.Context, id string) (CreditView, error)
}

type ListCredits interface {
	ListCredits(ctx context.Context) ([]*domain.CreditLine, error)
}

type ListEntries interface {
	ListEntries(ctx context.Context, id string) (EntriesView, error)
}

type PreviewAt interface {
	PreviewAt(ctx context.Context, id string, date time.Time) (domain.Preview, error)
}

// CreditService agrupa todos los puertos de entrada.
type CreditService interface {
	OpenCredit
	RegisterPurchase
	RegisterPayment
	LiquidateInterest
	GetCredit
	ListCredits
	ListEntries
	PreviewAt
}
