// Package usecases implementa los puertos de entrada orquestando dominio y
// repositorio: bloqueo -> idempotencia -> dominio -> persistir.
package usecases

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"leancore-fintech/backend/internal/application/ports"
	"leancore-fintech/backend/internal/domain"
)

const maxAccountNumberAttempts = 5

// Service implementa ports.CreditService.
type Service struct {
	repo  ports.CreditLineRepository
	clock ports.Clock
	ids   ports.IDGenerator
}

var _ ports.CreditService = (*Service)(nil)

func NewService(repo ports.CreditLineRepository, clock ports.Clock, ids ports.IDGenerator) *Service {
	return &Service{repo: repo, clock: clock, ids: ids}
}

// OpenCredit abre un crédito. Es idempotente por llave global.
func (s *Service) OpenCredit(ctx context.Context, cmd ports.OpenCreditCommand) (ports.OpenResult, error) {
	if strings.TrimSpace(cmd.IdempotencyKey) == "" {
		return ports.OpenResult{}, ports.ErrIdempotencyKeyRequired
	}
	hash := requestHash("open", struct {
		Holder      string
		Limit       domain.Money
		Rate        int
		Amount      domain.Money
		Date        string
		Description string
	}{cmd.Holder, cmd.LimitCents, cmd.RateEABps, cmd.AmountCents, cmd.DisbursementDate.Format(domain.DateLayout), cmd.Description})

	if res, ok, err := s.replayOpen(ctx, cmd.IdempotencyKey, hash); ok || err != nil {
		return res, err
	}

	for range maxAccountNumberAttempts {
		cl, e, err := domain.Open(domain.OpenParams{
			ID: s.ids.NewID(), AccountNumber: s.ids.NewAccountNumber(), Holder: cmd.Holder,
			Limit: cmd.LimitCents, RateEABps: cmd.RateEABps, DisbursementID: s.ids.NewID(),
			Disbursement: cmd.AmountCents, DisbursementDate: cmd.DisbursementDate, Description: cmd.Description,
		})
		if err != nil {
			return ports.OpenResult{}, err
		}
		e.IdempotencyKey, e.RequestHash = cmd.IdempotencyKey, hash
		switch err := s.repo.Create(ctx, cl, e, cmd.IdempotencyKey, hash); {
		case err == nil:
			return ports.OpenResult{Credit: cl, Entry: e}, nil
		case errors.Is(err, ports.ErrDuplicateAccountNumber):
			continue
		case errors.Is(err, ports.ErrDuplicateCreationKey):
			// Carrera con un reintento concurrente: gana el primero.
			res, _, err := s.replayOpen(ctx, cmd.IdempotencyKey, hash)
			return res, err
		default:
			return ports.OpenResult{}, err
		}
	}
	return ports.OpenResult{}, errors.New("no fue posible asignar un número de cuenta único")
}

func (s *Service) replayOpen(ctx context.Context, key, hash string) (ports.OpenResult, bool, error) {
	existing, err := s.repo.FindByCreationKey(ctx, key)
	if errors.Is(err, ports.ErrNotFound) {
		return ports.OpenResult{}, false, nil
	}
	if err != nil {
		return ports.OpenResult{}, false, err
	}
	if existing.RequestHash != hash {
		return ports.OpenResult{}, true, ports.ErrIdempotencyKeyReused
	}
	return ports.OpenResult{Credit: existing.Credit, Entry: existing.Entry, Replayed: true}, true, nil
}

// RegisterPurchase registra un consumo.
func (s *Service) RegisterPurchase(ctx context.Context, cmd ports.MovementCommand) (ports.MovementResult, error) {
	hash := requestHash("purchase", movementPayload(cmd))
	return s.runCommand(ctx, cmd.CreditID, cmd.IdempotencyKey, hash,
		func(_ ports.LockedCreditLine, cl *domain.CreditLine) ([]domain.Entry, error) {
			e, err := cl.Purchase(cmd.Date, cmd.AmountCents, cmd.Description, s.ids.NewID)
			return []domain.Entry{e}, err
		})
}

// RegisterPayment registra un pago (liquida interés, interés primero, FIFO).
func (s *Service) RegisterPayment(ctx context.Context, cmd ports.MovementCommand) (ports.MovementResult, error) {
	hash := requestHash("payment", movementPayload(cmd))
	return s.runCommand(ctx, cmd.CreditID, cmd.IdempotencyKey, hash,
		func(tx ports.LockedCreditLine, cl *domain.CreditLine) ([]domain.Entry, error) {
			uses, err := tx.OpenUses(ctx)
			if err != nil {
				return nil, err
			}
			return cl.Pay(cmd.Date, cmd.AmountCents, cmd.Description, uses, s.ids.NewID)
		})
}

// LiquidateInterest liquida el interés devengado a una fecha.
func (s *Service) LiquidateInterest(ctx context.Context, cmd ports.LiquidateCommand) (ports.MovementResult, error) {
	hash := requestHash("liquidate", cmd.Date.Format(domain.DateLayout))
	return s.runCommand(ctx, cmd.CreditID, cmd.IdempotencyKey, hash,
		func(_ ports.LockedCreditLine, cl *domain.CreditLine) ([]domain.Entry, error) {
			e, err := cl.LiquidateInterest(cmd.Date, s.ids.NewID)
			return []domain.Entry{e}, err
		})
}

type operation func(tx ports.LockedCreditLine, cl *domain.CreditLine) ([]domain.Entry, error)

// runCommand aplica el flujo común: bloqueo -> idempotencia -> dominio -> persistir.
// El movimiento principal (el último) guarda la llave y el hash.
func (s *Service) runCommand(ctx context.Context, creditID, key, hash string, op operation) (ports.MovementResult, error) {
	if strings.TrimSpace(key) == "" {
		return ports.MovementResult{}, ports.ErrIdempotencyKeyRequired
	}
	var res ports.MovementResult
	err := s.repo.WithLock(ctx, creditID, func(tx ports.LockedCreditLine) error {
		prev, err := tx.FindEntryByKey(ctx, key)
		if err != nil {
			return err
		}
		if prev != nil {
			if prev.RequestHash != hash {
				return ports.ErrIdempotencyKeyReused
			}
			res = ports.MovementResult{Entry: *prev, Replayed: true}
			return nil
		}
		cl := tx.Line().Clone()
		entries, err := op(tx, cl)
		if err != nil {
			return err
		}
		main := &entries[len(entries)-1]
		main.IdempotencyKey, main.RequestHash = key, hash
		if err := tx.Save(ctx, cl, entries); err != nil {
			return err
		}
		res = ports.MovementResult{Entry: *main}
		return nil
	})
	return res, err
}

// GetCredit devuelve el crédito con el interés devengado a hoy (sin escribir).
func (s *Service) GetCredit(ctx context.Context, id string) (ports.CreditView, error) {
	cl, err := s.repo.Get(ctx, id)
	if err != nil {
		return ports.CreditView{}, err
	}
	p, err := cl.ProjectionAt(s.clock.Today())
	if err != nil {
		return ports.CreditView{}, err
	}
	return ports.CreditView{Credit: cl, Projection: p}, nil
}

func (s *Service) ListCredits(ctx context.Context) ([]*domain.CreditLine, error) {
	return s.repo.List(ctx)
}

// ListEntries devuelve el historial del más reciente al más antiguo.
func (s *Service) ListEntries(ctx context.Context, id string) (ports.EntriesView, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return ports.EntriesView{}, err
	}
	entries, err := s.repo.ListEntries(ctx, id)
	if err != nil {
		return ports.EntriesView{}, err
	}
	var view ports.EntriesView
	for _, e := range entries {
		total := &view.TotalDebit
		if e.Type == domain.EntryPayment {
			total = &view.TotalCredit
		}
		if *total, err = total.Add(e.Amount); err != nil {
			return ports.EntriesView{}, err
		}
	}
	slices.Reverse(entries)
	view.Entries = entries
	return view, nil
}

// PreviewAt devuelve la vista previa de liquidación a una fecha (sin escribir).
func (s *Service) PreviewAt(ctx context.Context, id string, date time.Time) (domain.Preview, error) {
	cl, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.Preview{}, err
	}
	return cl.PreviewAt(date)
}

func movementPayload(cmd ports.MovementCommand) any {
	return struct {
		Amount      domain.Money
		Date        string
		Description string
	}{cmd.AmountCents, cmd.Date.Format(domain.DateLayout), cmd.Description}
}

// requestHash es el SHA-256 de la representación canónica del comando
// (sin la llave). json.Marshal de un struct es determinista.
func requestHash(kind string, payload any) string {
	b, _ := json.Marshal(struct {
		Kind    string
		Payload any
	}{kind, payload})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
