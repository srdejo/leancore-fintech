package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"leancore-fintech/backend/internal/application/ports"
	"leancore-fintech/backend/internal/domain"
)

// Repository implementa ports.CreditLineRepository con pgx.
type Repository struct {
	pool *pgxpool.Pool
}

var _ ports.CreditLineRepository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// querier es la parte común de *pgxpool.Pool y pgx.Tx.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const lineColumns = `id::text, account_number, holder, limit_cents, rate_ea_bps, daily_rate_e15,
	capital_cents, interest_due_cents, accrual_units::text, last_accrual_date, anchor_date,
	last_entry_date, last_seq`

const entryColumns = `id::text, credit_line_id::text, seq, type, amount_cents, entry_date, description, detail,
	COALESCE(to_interest_cents, 0), balance_after_cents, COALESCE(idempotency_key::text, ''),
	COALESCE(request_hash, '')`

func (r *Repository) Create(ctx context.Context, cl *domain.CreditLine, e domain.Entry, key, hash string) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO credit_lines (id, account_number, holder, limit_cents, rate_ea_bps,
			daily_rate_e15, capital_cents, interest_due_cents, accrual_units, last_accrual_date, anchor_date,
			last_entry_date, last_seq, creation_idempotency_key, creation_request_hash)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::numeric,$10,$11,$12,$13,$14,$15)`,
			cl.ID, cl.AccountNumber, cl.Holder, int64(cl.Limit), cl.RateEABps, cl.DailyRateE15,
			int64(cl.Capital), int64(cl.InterestDue), cl.Accrual.String(), cl.LastAccrualDate, cl.AnchorDate,
			cl.LastEntryDate, cl.LastSeq, key, hash)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				switch pgErr.ConstraintName {
				case "credit_lines_creation_key_uq":
					return ports.ErrDuplicateCreationKey
				case "credit_lines_account_number_uq":
					return ports.ErrDuplicateAccountNumber
				}
			}
			return fmt.Errorf("insert credit line: %w", err)
		}
		return insertEntries(ctx, tx, []domain.Entry{e})
	})
}

func (r *Repository) FindByCreationKey(ctx context.Context, key string) (ports.CreatedCredit, error) {
	var id, hash string
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, creation_request_hash FROM credit_lines WHERE creation_idempotency_key = $1`, key).
		Scan(&id, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.CreatedCredit{}, ports.ErrNotFound
	}
	if err != nil {
		return ports.CreatedCredit{}, err
	}
	cl, err := getLine(ctx, r.pool, id, false)
	if err != nil {
		return ports.CreatedCredit{}, err
	}
	entries, err := queryEntries(ctx, r.pool, `WHERE credit_line_id = $1 AND seq = 1`, id)
	if err != nil {
		return ports.CreatedCredit{}, err
	}
	if len(entries) != 1 {
		return ports.CreatedCredit{}, errors.New("crédito sin desembolso inicial")
	}
	return ports.CreatedCredit{Credit: cl, Entry: entries[0], RequestHash: hash}, nil
}

// WithLock abre una transacción, bloquea la fila del saldo con
// SELECT ... FOR UPDATE y ejecuta fn. Si fn falla, hace rollback.
func (r *Repository) WithLock(ctx context.Context, id string, fn func(tx ports.LockedCreditLine) error) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		cl, err := getLine(ctx, tx, id, true)
		if err != nil {
			return err
		}
		return fn(&lockedLine{tx: tx, line: cl})
	})
}

func (r *Repository) Get(ctx context.Context, id string) (*domain.CreditLine, error) {
	return getLine(ctx, r.pool, id, false)
}

func (r *Repository) List(ctx context.Context) ([]*domain.CreditLine, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+lineColumns+` FROM credit_lines ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (*domain.CreditLine, error) { return scanLine(row) })
}

func (r *Repository) ListEntries(ctx context.Context, id string) ([]domain.Entry, error) {
	return queryEntries(ctx, r.pool, `WHERE credit_line_id = $1`, id)
}

type lockedLine struct {
	tx   pgx.Tx
	line *domain.CreditLine
}

func (l *lockedLine) Line() *domain.CreditLine { return l.line }

func (l *lockedLine) FindEntryByKey(ctx context.Context, key string) (*domain.Entry, error) {
	entries, err := queryEntries(ctx, l.tx, `WHERE credit_line_id = $1 AND idempotency_key = $2::uuid`, l.line.ID, key)
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	return &entries[0], nil
}

func (l *lockedLine) OpenUses(ctx context.Context) ([]domain.OpenUse, error) {
	rows, err := l.tx.Query(ctx, `
		SELECT e.id::text, e.seq, e.amount_cents - COALESCE(SUM(a.to_capital_cents), 0) AS pending
		FROM entries e
		LEFT JOIN payment_allocations a ON a.use_entry_id = e.id
		WHERE e.credit_line_id = $1 AND e.type IN ('DESEMBOLSO', 'CONSUMO')
		GROUP BY e.id, e.seq, e.amount_cents
		HAVING e.amount_cents - COALESCE(SUM(a.to_capital_cents), 0) > 0
		ORDER BY e.seq`, l.line.ID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.OpenUse, error) {
		var u domain.OpenUse
		var pending int64
		err := row.Scan(&u.EntryID, &u.Seq, &pending)
		u.Pending = domain.Money(pending)
		return u, err
	})
}

func (l *lockedLine) Save(ctx context.Context, cl *domain.CreditLine, entries []domain.Entry) error {
	tag, err := l.tx.Exec(ctx, `UPDATE credit_lines SET capital_cents = $2, interest_due_cents = $3,
		accrual_units = $4::numeric, last_accrual_date = $5, anchor_date = $6, last_entry_date = $7, last_seq = $8
		WHERE id = $1`,
		cl.ID, int64(cl.Capital), int64(cl.InterestDue), cl.Accrual.String(), cl.LastAccrualDate,
		cl.AnchorDate, cl.LastEntryDate, cl.LastSeq)
	if err != nil {
		return fmt.Errorf("update credit line: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ports.ErrNotFound
	}
	return insertEntries(ctx, l.tx, entries)
}

func insertEntries(ctx context.Context, q querier, entries []domain.Entry) error {
	for _, e := range entries {
		var toInterest *int64
		if e.Type == domain.EntryPayment {
			v := int64(e.ToInterest)
			toInterest = &v
		}
		_, err := q.Exec(ctx, `INSERT INTO entries (id, credit_line_id, seq, type, amount_cents, entry_date,
			description, detail, to_interest_cents, balance_after_cents, idempotency_key, request_hash)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,'')::uuid,NULLIF($12,''))`,
			e.ID, e.CreditLineID, e.Seq, string(e.Type), int64(e.Amount), e.Date, e.Description, e.Detail,
			toInterest, int64(e.BalanceAfter), e.IdempotencyKey, e.RequestHash)
		if err != nil {
			return fmt.Errorf("insert entry: %w", err)
		}
		for _, a := range e.Allocations {
			if _, err := q.Exec(ctx, `INSERT INTO payment_allocations (payment_entry_id, use_entry_id, to_capital_cents)
				VALUES ($1,$2,$3)`, e.ID, a.UseEntryID, int64(a.ToCapital)); err != nil {
				return fmt.Errorf("insert allocation: %w", err)
			}
		}
	}
	return nil
}

func getLine(ctx context.Context, q querier, id string, forUpdate bool) (*domain.CreditLine, error) {
	sql := `SELECT ` + lineColumns + ` FROM credit_lines WHERE id = $1::uuid`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	cl, err := scanLine(q.QueryRow(ctx, sql, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrNotFound
	}
	return cl, err
}

func scanLine(row pgx.Row) (*domain.CreditLine, error) {
	var cl domain.CreditLine
	var limit, capital, due int64
	var accrual string
	err := row.Scan(&cl.ID, &cl.AccountNumber, &cl.Holder, &limit, &cl.RateEABps, &cl.DailyRateE15,
		&capital, &due, &accrual, &cl.LastAccrualDate, &cl.AnchorDate, &cl.LastEntryDate, &cl.LastSeq)
	if err != nil {
		return nil, err
	}
	acc, ok := new(big.Int).SetString(accrual, 10)
	if !ok {
		return nil, fmt.Errorf("accrual_units inválido: %q", accrual)
	}
	cl.Limit, cl.Capital, cl.InterestDue, cl.Accrual = domain.Money(limit), domain.Money(capital), domain.Money(due), acc
	cl.LastAccrualDate, cl.AnchorDate, cl.LastEntryDate = utcDate(cl.LastAccrualDate), utcDate(cl.AnchorDate), utcDate(cl.LastEntryDate)
	return &cl, nil
}

// queryEntries carga movimientos (ordenados por seq) con sus asignaciones.
func queryEntries(ctx context.Context, q querier, where string, args ...any) ([]domain.Entry, error) {
	rows, err := q.Query(ctx, `SELECT `+entryColumns+` FROM entries `+where+` ORDER BY seq`, args...)
	if err != nil {
		return nil, err
	}
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Entry, error) {
		var e domain.Entry
		var typ string
		var amount, toInterest, balance int64
		err := row.Scan(&e.ID, &e.CreditLineID, &e.Seq, &typ, &amount, &e.Date, &e.Description, &e.Detail,
			&toInterest, &balance, &e.IdempotencyKey, &e.RequestHash)
		e.Type, e.Amount, e.ToInterest, e.BalanceAfter = domain.EntryType(typ), domain.Money(amount), domain.Money(toInterest), domain.Money(balance)
		e.Date = utcDate(e.Date)
		return e, err
	})
	if err != nil || len(entries) == 0 {
		return entries, err
	}

	ids := make([]string, 0, len(entries))
	index := map[string]int{}
	for i, e := range entries {
		if e.Type == domain.EntryPayment {
			ids = append(ids, e.ID)
			index[e.ID] = i
		}
	}
	if len(ids) == 0 {
		return entries, nil
	}
	arows, err := q.Query(ctx, `SELECT a.payment_entry_id::text, a.use_entry_id::text, u.seq, a.to_capital_cents
		FROM payment_allocations a JOIN entries u ON u.id = a.use_entry_id
		WHERE a.payment_entry_id = ANY($1::uuid[]) ORDER BY u.seq`, ids)
	if err != nil {
		return nil, err
	}
	defer arows.Close()
	for arows.Next() {
		var payID string
		var a domain.Allocation
		var amt int64
		if err := arows.Scan(&payID, &a.UseEntryID, &a.UseSeq, &amt); err != nil {
			return nil, err
		}
		a.ToCapital = domain.Money(amt)
		i := index[payID]
		entries[i].Allocations = append(entries[i].Allocations, a)
	}
	return entries, arows.Err()
}

func utcDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
