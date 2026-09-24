package usecases

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"leancore-fintech/backend/internal/adapters/out/memory"
	"leancore-fintech/backend/internal/application/ports"
	"leancore-fintech/backend/internal/domain"
)

func date(s string) time.Time {
	t, _ := time.Parse(domain.DateLayout, s)
	return t
}

func setup(t *testing.T) (*Service, *memory.Repository, string) {
	t.Helper()
	repo := memory.NewRepository()
	svc := NewService(repo, fixedClock{date("2026-07-01")}, &seqIDs{})
	res, err := svc.OpenCredit(context.Background(), ports.OpenCreditCommand{
		IdempotencyKey: "open-1", Holder: "María", LimitCents: 1_000_000_000, RateEABps: 2400,
		AmountCents: 500_000_000, DisbursementDate: date("2026-06-01"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, repo, res.Credit.ID
}

func pay(id, key string, amount domain.Money) ports.MovementCommand {
	return ports.MovementCommand{CreditID: id, IdempotencyKey: key, AmountCents: amount, Date: date("2026-06-10")}
}

func TestReintentoDelMismoPago(t *testing.T) {
	svc, repo, id := setup(t)
	ctx := context.Background()
	first, err := svc.RegisterPayment(ctx, pay(id, "k1", 50_000_000))
	if err != nil {
		t.Fatal(err)
	}
	n := repo.EntryCount(id)
	second, err := svc.RegisterPayment(ctx, pay(id, "k1", 50_000_000))
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || first.Replayed || second.Entry.ID != first.Entry.ID || repo.EntryCount(id) != n {
		t.Fatalf("no fue idempotente: %+v / %+v", first, second)
	}
	if len(second.Entry.Allocations) != len(first.Entry.Allocations) || second.Entry.ToInterest != first.Entry.ToInterest {
		t.Fatal("el resultado repetido difiere del original")
	}
}

func TestLlaveReutilizadaConOtroContenido(t *testing.T) {
	svc, repo, id := setup(t)
	ctx := context.Background()
	if _, err := svc.RegisterPayment(ctx, pay(id, "k1", 50_000_000)); err != nil {
		t.Fatal(err)
	}
	n := repo.EntryCount(id)
	_, err := svc.RegisterPayment(ctx, pay(id, "k1", 60_000_000))
	if !errors.Is(err, ports.ErrIdempotencyKeyReused) || repo.EntryCount(id) != n {
		t.Fatalf("err = %v", err)
	}
	// La misma llave en otro tipo de comando también es "otro contenido".
	_, err = svc.RegisterPurchase(ctx, pay(id, "k1", 50_000_000))
	if !errors.Is(err, ports.ErrIdempotencyKeyReused) {
		t.Fatalf("err = %v", err)
	}
}

func TestRechazoNoConsumeLaLlave(t *testing.T) {
	svc, _, id := setup(t)
	ctx := context.Background()
	_, err := svc.RegisterPayment(ctx, pay(id, "k1", 900_000_000))
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodePaymentExceedsBalance {
		t.Fatalf("err = %v", err)
	}
	res, err := svc.RegisterPayment(ctx, pay(id, "k1", 10_000_000))
	if err != nil || res.Replayed {
		t.Fatalf("el pago válido debía registrarse: %v %+v", err, res)
	}
}

func TestComandoSinLlave(t *testing.T) {
	svc, _, id := setup(t)
	ctx := context.Background()
	if _, err := svc.RegisterPurchase(ctx, pay(id, " ", 1)); !errors.Is(err, ports.ErrIdempotencyKeyRequired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.OpenCredit(ctx, ports.OpenCreditCommand{}); !errors.Is(err, ports.ErrIdempotencyKeyRequired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.LiquidateInterest(ctx, ports.LiquidateCommand{CreditID: id}); !errors.Is(err, ports.ErrIdempotencyKeyRequired) {
		t.Fatalf("err = %v", err)
	}
}

func TestReintentosConcurrentesMismaLlave(t *testing.T) {
	svc, repo, id := setup(t)
	n := repo.EntryCount(id)
	var wg sync.WaitGroup
	results := make([]ports.MovementResult, 10)
	for i := range results {
		wg.Go(func() {
			r, err := svc.RegisterPurchase(context.Background(), pay(id, "k-conc", 1_000))
			if err != nil {
				t.Error(err)
			}
			results[i] = r
		})
	}
	wg.Wait()
	if repo.EntryCount(id) != n+1 {
		t.Fatalf("movimientos = %d, want %d", repo.EntryCount(id), n+1)
	}
	for _, r := range results {
		if r.Entry.ID != results[0].Entry.ID {
			t.Fatal("respuestas distintas")
		}
	}
}

func TestAperturaIdempotente(t *testing.T) {
	svc, repo, _ := setup(t)
	ctx := context.Background()
	cmd := ports.OpenCreditCommand{IdempotencyKey: "open-1", Holder: "María", LimitCents: 1_000_000_000,
		RateEABps: 2400, AmountCents: 500_000_000, DisbursementDate: date("2026-06-01")}
	res, err := svc.OpenCredit(ctx, cmd)
	if err != nil || !res.Replayed || repo.Len() != 1 {
		t.Fatalf("apertura repetida: %v %+v", err, res)
	}
	cmd.AmountCents = 1
	if _, err := svc.OpenCredit(ctx, cmd); !errors.Is(err, ports.ErrIdempotencyKeyReused) {
		t.Fatalf("err = %v", err)
	}
	// Un rechazo de negocio en la apertura tampoco consume la llave.
	bad := cmd
	bad.IdempotencyKey, bad.AmountCents = "open-2", 2_000_000_000
	if _, err := svc.OpenCredit(ctx, bad); err == nil {
		t.Fatal("esperaba rechazo")
	}
	bad.AmountCents = 1_000
	if res, err := svc.OpenCredit(ctx, bad); err != nil || res.Replayed {
		t.Fatalf("err = %v", err)
	}
}

func TestPagoConInteresGuardaLaLlaveEnElPago(t *testing.T) {
	svc, repo, id := setup(t)
	res, err := svc.RegisterPayment(context.Background(), pay(id, "k1", 50_000_000))
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry.Type != domain.EntryPayment || res.Entry.IdempotencyKey != "k1" {
		t.Fatalf("%+v", res.Entry)
	}
	entries, _ := repo.ListEntries(context.Background(), id)
	if entries[1].Type != domain.EntryInterest || entries[1].IdempotencyKey != "" {
		t.Fatalf("el INTERES automático no debe llevar la llave: %+v", entries[1])
	}
}

func TestCreditoInexistente(t *testing.T) {
	svc, _, _ := setup(t)
	ctx := context.Background()
	if _, err := svc.GetCredit(ctx, "nope"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.RegisterPurchase(ctx, pay("nope", "k", 1)); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// --- 4.3 Consultas ---------------------------------------------------------

func TestProyeccionAHoySinEscribir(t *testing.T) {
	svc, repo, id := setup(t)
	n, saves := repo.EntryCount(id), repo.SaveCalls()
	view, err := svc.GetCredit(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	// Reloj fijo 2026-07-01: 30 días de devengo sobre 500000000.
	want, _ := view.Credit.PreviewAt(date("2026-07-01"))
	if view.Projection.InterestToLiquidate != want.InterestToLiquidate || want.InterestToLiquidate == 0 {
		t.Fatalf("proyección %+v", view.Projection)
	}
	if view.Projection.TotalBalance != 500_000_000+want.InterestToLiquidate {
		t.Fatalf("saldo proyectado %d", view.Projection.TotalBalance)
	}
	if repo.EntryCount(id) != n || repo.SaveCalls() != saves || view.Credit.InterestDue != 0 {
		t.Fatal("la consulta escribió")
	}
}

func TestProyeccionConRelojAnteriorAlUltimoMovimiento(t *testing.T) {
	repo := memory.NewRepository()
	svc := NewService(repo, fixedClock{date("2026-01-01")}, &seqIDs{})
	res, _ := svc.OpenCredit(context.Background(), ports.OpenCreditCommand{IdempotencyKey: "k", Holder: "X",
		LimitCents: 1000, RateEABps: 2400, AmountCents: 500, DisbursementDate: date("2026-06-01")})
	view, err := svc.GetCredit(context.Background(), res.Credit.ID)
	if err != nil || view.Projection.InterestToLiquidate != 0 {
		t.Fatalf("%v %+v", err, view.Projection)
	}
}

func TestHistorialYTotales(t *testing.T) {
	svc, _, id := setup(t)
	ctx := context.Background()
	if _, err := svc.RegisterPurchase(ctx, ports.MovementCommand{CreditID: id, IdempotencyKey: "a",
		AmountCents: 80_000_000, Date: date("2026-06-15")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RegisterPayment(ctx, ports.MovementCommand{CreditID: id, IdempotencyKey: "b",
		AmountCents: 120_000_000, Date: date("2026-07-01")}); err != nil {
		t.Fatal(err)
	}
	view, err := svc.ListEntries(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	types := []domain.EntryType{domain.EntryPayment, domain.EntryInterest, domain.EntryPurchase, domain.EntryDisbursement}
	for i, e := range view.Entries {
		if e.Type != types[i] {
			t.Fatalf("orden: %v", view.Entries)
		}
	}
	interest := view.Entries[1].Amount
	if view.TotalDebit != 580_000_000+interest || view.TotalCredit != 120_000_000 {
		t.Fatalf("totales %d %d", view.TotalDebit, view.TotalCredit)
	}
	if len(view.Entries[0].Allocations) == 0 {
		t.Fatal("el pago no trae asignaciones")
	}
}

func TestVistaPreviaSinEscribir(t *testing.T) {
	svc, repo, id := setup(t)
	n := repo.EntryCount(id)
	p, err := svc.PreviewAt(context.Background(), id, date("2026-09-01"))
	if err != nil || p.InterestToLiquidate == 0 || p.DaysSinceAnchor != 92 || repo.EntryCount(id) != n {
		t.Fatalf("%v %+v", err, p)
	}
}
