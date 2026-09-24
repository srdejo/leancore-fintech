package postgres

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"leancore-fintech/backend/internal/adapters/out/ids"
	"leancore-fintech/backend/internal/application/ports"
	"leancore-fintech/backend/internal/application/usecases"
	"leancore-fintech/backend/internal/domain"
)

// Tests de integración contra un PostgreSQL real (testcontainers). Se omiten
// con -short o si Docker no está disponible.

var (
	testDSN  string
	testPool *pgxpool.Pool
)

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() || os.Getenv("SKIP_INTEGRATION") != "" {
		os.Exit(m.Run())
	}
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("ledger"), tcpostgres.WithUsername("ledger"), tcpostgres.WithPassword("ledger"),
		tcpostgres.BasicWaitStrategies())
	if err != nil {
		fmt.Fprintln(os.Stderr, "sin Docker, se omiten tests de integración:", err)
		os.Exit(m.Run())
	}
	testDSN, err = ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(err)
	}
	if err := Migrate(testDSN); err != nil {
		panic(err)
	}
	testPool, err = pgxpool.New(ctx, testDSN)
	if err != nil {
		panic(err)
	}
	code := m.Run()
	testPool.Close()
	_ = testcontainers.TerminateContainer(ctr)
	os.Exit(code)
}

func needDB(t *testing.T) {
	t.Helper()
	if testPool == nil {
		t.Skip("requiere PostgreSQL (Docker)")
	}
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Today() time.Time { return c.t }

func day(s string) time.Time {
	t, _ := time.Parse(domain.DateLayout, s)
	return t
}

func newService() (*usecases.Service, *Repository) {
	repo := NewRepository(testPool)
	return usecases.NewService(repo, fixedClock{day("2026-09-01")}, ids.Generator{}), repo
}

func openCredit(t *testing.T, svc *usecases.Service, limit, amount domain.Money) string {
	t.Helper()
	res, err := svc.OpenCredit(context.Background(), ports.OpenCreditCommand{IdempotencyKey: uuid.NewString(),
		Holder: "María Fernanda Ruiz", LimitCents: limit, RateEABps: 2400, AmountCents: amount,
		DisbursementDate: day("2026-06-01")})
	if err != nil {
		t.Fatal(err)
	}
	return res.Credit.ID
}

// --- 5.1 Migraciones ---------------------------------------------------------

func TestMigracionesSubenYBajan(t *testing.T) {
	needDB(t)
	// Base aislada para no interferir con los demás tests.
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `CREATE DATABASE migtest`); err != nil {
		t.Fatal(err)
	}
	dsn := replaceDB(t, testDSN, "migtest")
	if err := Migrate(dsn); err != nil {
		t.Fatal(err)
	}
	if err := MigrateDown(dsn); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(dsn); err != nil {
		t.Fatal("volver a subir:", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables
		WHERE table_name IN ('credit_lines','entries','payment_allocations')`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("tablas = %d, %v", n, err)
	}
}

func replaceDB(t *testing.T, dsn, db string) string {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	c := cfg.ConnConfig
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable", c.User, c.Password, c.Host, c.Port, db)
}

func TestRestriccionesDelEsquema(t *testing.T) {
	needDB(t)
	svc, _ := newService()
	id := openCredit(t, svc, 1_000_000, 1_000)
	ctx := context.Background()
	// amount > 0
	_, err := testPool.Exec(ctx, `INSERT INTO entries (id, credit_line_id, seq, type, amount_cents, entry_date,
		description, balance_after_cents) VALUES ($1, $2, 99, 'CONSUMO', 0, '2026-06-01', 'x', 0)`, uuid.NewString(), id)
	if err == nil {
		t.Fatal("amount_cents = 0 debía rechazarse")
	}
	// UNIQUE(credit_line_id, seq)
	_, err = testPool.Exec(ctx, `INSERT INTO entries (id, credit_line_id, seq, type, amount_cents, entry_date,
		description, balance_after_cents) VALUES ($1, $2, 1, 'CONSUMO', 5, '2026-06-01', 'x', 0)`, uuid.NewString(), id)
	if err == nil {
		t.Fatal("seq duplicado debía rechazarse")
	}
}

// --- 5.2 Repositorio ---------------------------------------------------------

func TestIdaYVuelta(t *testing.T) {
	needDB(t)
	svc, repo := newService()
	ctx := context.Background()
	id := openCredit(t, svc, 1_000_000_000, 500_000_000)
	mustOK := func(_ ports.MovementResult, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	mustOK(svc.RegisterPurchase(ctx, ports.MovementCommand{CreditID: id, IdempotencyKey: uuid.NewString(),
		AmountCents: 80_000_000, Date: day("2026-06-15"), Description: "Compra supermercado"}))
	payRes, err := svc.RegisterPayment(ctx, ports.MovementCommand{CreditID: id, IdempotencyKey: uuid.NewString(),
		AmountCents: 520_000_000, Date: day("2026-07-01")})
	if err != nil {
		t.Fatal(err)
	}

	cl, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	// Mismo escenario que el test de dominio: interés 9597385 al 07-01.
	if cl.InterestDue != 0 || cl.Capital != 580_000_000+9_597_385-520_000_000 || cl.Accrual.Sign() != 0 || cl.LastSeq != 4 {
		t.Fatalf("línea: %+v", cl)
	}
	if !cl.LastEntryDate.Equal(day("2026-07-01")) || cl.DailyRateE15 != 589_519_944_146 {
		t.Fatalf("fechas/tasa: %+v", cl)
	}
	entries, err := repo.ListEntries(ctx, id)
	if err != nil || len(entries) != 4 {
		t.Fatalf("%v %d", err, len(entries))
	}
	pay := entries[3]
	if pay.ID != payRes.Entry.ID || pay.ToInterest != 9_597_385 || !reflect.DeepEqual(pay.Allocations, payRes.Entry.Allocations) {
		t.Fatalf("pago leído %+v vs %+v", pay, payRes.Entry)
	}
	// FIFO: 510402615 a capital, todo al desembolso (500000000 pendientes) y el
	// resto al consumo.
	if len(pay.Allocations) != 2 || pay.Allocations[0].ToCapital != 500_000_000 || pay.Allocations[1].ToCapital != 10_402_615 {
		t.Fatalf("asignaciones %+v", pay.Allocations)
	}
	if entries[1].Description != "Compra supermercado" || entries[2].Type != domain.EntryInterest || entries[2].Detail == "" {
		t.Fatalf("movimientos %+v", entries)
	}

	// El devengo NUMERIC(38,0) ida y vuelta con un valor > int64.
	huge, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	err = repo.WithLock(ctx, id, func(tx ports.LockedCreditLine) error {
		l := tx.Line().Clone()
		l.Accrual = huge
		return tx.Save(ctx, l, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	cl, _ = repo.Get(ctx, id)
	if cl.Accrual.Cmp(huge) != 0 {
		t.Fatalf("accrual %s", cl.Accrual)
	}
}

func TestRollbackAnteError(t *testing.T) {
	needDB(t)
	svc, repo := newService()
	ctx := context.Background()
	id := openCredit(t, svc, 1_000_000_000, 500_000_000)
	before, _ := repo.Get(ctx, id)
	boom := errors.New("falla simulada")
	err := repo.WithLock(ctx, id, func(tx ports.LockedCreditLine) error {
		l := tx.Line().Clone()
		e, err := l.Purchase(day("2026-06-02"), 1_000, "", ids.Generator{}.NewID)
		if err != nil {
			return err
		}
		if err := tx.Save(ctx, l, []domain.Entry{e}); err != nil {
			return err
		}
		return boom // falla después de escribir
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	after, _ := repo.Get(ctx, id)
	entries, _ := repo.ListEntries(ctx, id)
	if !reflect.DeepEqual(before, after) || len(entries) != 1 {
		t.Fatalf("quedaron efectos: %+v, %d movimientos", after, len(entries))
	}
}

func TestCreditoInexistenteYLlaveDeApertura(t *testing.T) {
	needDB(t)
	svc, repo := newService()
	ctx := context.Background()
	if _, err := repo.Get(ctx, uuid.NewString()); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	cmd := ports.OpenCreditCommand{IdempotencyKey: uuid.NewString(), Holder: "X", LimitCents: 100,
		RateEABps: 0, AmountCents: 50, DisbursementDate: day("2026-06-01")}
	a, err := svc.OpenCredit(ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.OpenCredit(ctx, cmd)
	if err != nil || !b.Replayed || b.Credit.ID != a.Credit.ID || b.Entry.ID != a.Entry.ID {
		t.Fatalf("apertura repetida: %v %+v", err, b)
	}
}

// --- 5.3 Concurrencia --------------------------------------------------------

func TestConsumosConcurrentesNoSuperanElCupo(t *testing.T) {
	needDB(t)
	svc, repo := newService()
	ctx := context.Background()
	// Disponible 40000000; 10 consumos de 30000000 en paralelo: solo uno cabe.
	id := openCredit(t, svc, 1_000_000_000, 960_000_000)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok, rejected := 0, 0
	for range 10 {
		wg.Go(func() {
			_, err := svc.RegisterPurchase(ctx, ports.MovementCommand{CreditID: id, IdempotencyKey: uuid.NewString(),
				AmountCents: 30_000_000, Date: day("2026-06-01")})
			mu.Lock()
			defer mu.Unlock()
			var de *domain.Error
			switch {
			case err == nil:
				ok++
			case errors.As(err, &de) && de.Code == domain.CodeInsufficientAvailable:
				rejected++
			default:
				t.Error(err)
			}
		})
	}
	wg.Wait()
	cl, _ := repo.Get(ctx, id)
	if ok != 1 || rejected != 9 || cl.Available() != 10_000_000 {
		t.Fatalf("ok=%d rechazados=%d disponible=%d", ok, rejected, cl.Available())
	}
}

func TestPagosConcurrentesNoSuperanElSaldo(t *testing.T) {
	needDB(t)
	svc, repo := newService()
	ctx := context.Background()
	// Tasa 0 para que el saldo sea exactamente 50000000.
	res, err := svc.OpenCredit(ctx, ports.OpenCreditCommand{IdempotencyKey: uuid.NewString(), Holder: "X",
		LimitCents: 100_000_000, RateEABps: 0, AmountCents: 50_000_000, DisbursementDate: day("2026-06-01")})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Credit.ID
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for range 2 {
		wg.Go(func() {
			_, err := svc.RegisterPayment(ctx, ports.MovementCommand{CreditID: id, IdempotencyKey: uuid.NewString(),
				AmountCents: 30_000_000, Date: day("2026-06-01")})
			mu.Lock()
			defer mu.Unlock()
			var de *domain.Error
			if err == nil {
				ok++
			} else if !errors.As(err, &de) || de.Code != domain.CodePaymentExceedsBalance {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	cl, _ := repo.Get(ctx, id)
	if ok != 1 || cl.Balance() != 20_000_000 {
		t.Fatalf("ok=%d saldo=%d", ok, cl.Balance())
	}
}

func TestReintentosConcurrentesConLaMismaLlave(t *testing.T) {
	needDB(t)
	svc, repo := newService()
	ctx := context.Background()
	id := openCredit(t, svc, 1_000_000_000, 500_000_000)
	key := uuid.NewString()
	var wg sync.WaitGroup
	results := make([]ports.MovementResult, 8)
	for i := range results {
		wg.Go(func() {
			r, err := svc.RegisterPayment(ctx, ports.MovementCommand{CreditID: id, IdempotencyKey: key,
				AmountCents: 10_000_000, Date: day("2026-07-01")})
			if err != nil {
				t.Error(err)
			}
			results[i] = r
		})
	}
	wg.Wait()
	entries, _ := repo.ListEntries(ctx, id)
	payments := 0
	for _, e := range entries {
		if e.Type == domain.EntryPayment {
			payments++
		}
	}
	if payments != 1 {
		t.Fatalf("pagos = %d", payments)
	}
	for _, r := range results {
		if r.Entry.ID != results[0].Entry.ID {
			t.Fatal("respuestas distintas")
		}
	}
}
