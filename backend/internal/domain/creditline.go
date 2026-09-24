package domain

import (
	"math/big"
	"strings"
	"time"
)

// EntryType es el tipo de un movimiento del libro.
type EntryType string

const (
	EntryDisbursement EntryType = "DESEMBOLSO"
	EntryPurchase     EntryType = "CONSUMO"
	EntryInterest     EntryType = "INTERES"
	EntryPayment      EntryType = "PAGO"
)

// Status es el estado derivado del saldo de un crédito.
type Status string

const (
	StatusCurrent   Status = "AL_DIA"
	StatusOverdrawn Status = "SOBREGIRADO"
	StatusNoBalance Status = "SIN_SALDO"
)

// Allocation es la porción de capital de un pago aplicada a un uso.
type Allocation struct {
	UseEntryID string
	UseSeq     int
	ToCapital  Money
}

// Entry es un movimiento inmutable del libro.
type Entry struct {
	ID             string
	CreditLineID   string
	Seq            int
	Type           EntryType
	Amount         Money
	Date           time.Time
	Description    string
	Detail         string
	ToInterest     Money // solo pagos
	Allocations    []Allocation
	BalanceAfter   Money
	IdempotencyKey string
	RequestHash    string
}

// OpenUse es un uso (desembolso o consumo) con capital pendiente.
type OpenUse struct {
	EntryID string
	Seq     int
	Pending Money
}

// CreditLine es el agregado: condiciones fijas y saldo materializado.
type CreditLine struct {
	ID              string
	AccountNumber   string
	Holder          string
	Limit           Money
	RateEABps       int
	DailyRateE15    int64
	Capital         Money
	InterestDue     Money
	Accrual         *big.Int  // devengo sin liquidar, unidades de 10^-15 centavos
	LastAccrualDate time.Time // hasta dónde está devengado Accrual
	AnchorDate      time.Time // última liquidación (o apertura)
	LastEntryDate   time.Time
	LastSeq         int
}

// NewIDFunc genera identificadores para movimientos nuevos.
type NewIDFunc func() string

// OpenParams son los datos de apertura de un crédito.
type OpenParams struct {
	ID               string
	AccountNumber    string
	Holder           string
	Limit            Money
	RateEABps        int
	DisbursementID   string
	Disbursement     Money
	DisbursementDate time.Time
	Description      string
}

// Open abre un crédito con su desembolso inicial obligatorio.
func Open(p OpenParams) (*CreditLine, Entry, error) {
	holder := strings.TrimSpace(p.Holder)
	switch {
	case holder == "":
		return nil, Entry{}, validationError("holder", "Indica el nombre del titular.")
	case p.Limit <= 0:
		return nil, Entry{}, validationError("limitCents", "El cupo aprobado debe ser mayor que cero.")
	case p.RateEABps < 0 || p.RateEABps > MaxRateEABps:
		return nil, Entry{}, validationError("rateEaBps", "La tasa EA está fuera del rango permitido.")
	case p.Disbursement <= 0:
		return nil, Entry{}, validationError("amountCents", "El desembolso inicial es obligatorio y debe ser mayor que cero.")
	case p.DisbursementDate.IsZero():
		return nil, Entry{}, validationError("date", "Indica la fecha del desembolso.")
	case p.Disbursement > p.Limit:
		return nil, Entry{}, newError(CodeDisbursementExceeds, "El desembolso supera el cupo aprobado.",
			map[string]any{"limitCents": p.Limit})
	}
	daily, err := DailyRateE15(p.RateEABps)
	if err != nil {
		return nil, Entry{}, validationError("rateEaBps", "La tasa EA está fuera del rango permitido.")
	}
	date := truncateDate(p.DisbursementDate)
	cl := &CreditLine{
		ID:              p.ID,
		AccountNumber:   p.AccountNumber,
		Holder:          holder,
		Limit:           p.Limit,
		RateEABps:       p.RateEABps,
		DailyRateE15:    daily,
		Capital:         p.Disbursement,
		Accrual:         new(big.Int),
		LastAccrualDate: date,
		AnchorDate:      date,
		LastEntryDate:   date,
		LastSeq:         1,
	}
	desc := strings.TrimSpace(p.Description)
	if desc == "" {
		desc = "Desembolso inicial del crédito"
	}
	e := Entry{
		ID: p.DisbursementID, CreditLineID: cl.ID, Seq: 1, Type: EntryDisbursement,
		Amount: p.Disbursement, Date: date, Description: desc, BalanceAfter: cl.Balance(),
	}
	return cl, e, nil
}

// Balance es el saldo pendiente: capital + interés por pagar.
func (c *CreditLine) Balance() Money { return c.Capital + c.InterestDue }

// Available es max(0, cupo - capital - interés por pagar).
func (c *CreditLine) Available() Money { return maxMoney(0, c.Limit-c.Balance()) }

// Status deriva el estado del saldo.
func (c *CreditLine) Status() Status {
	switch b := c.Balance(); {
	case b > c.Limit:
		return StatusOverdrawn
	case b == 0:
		return StatusNoBalance
	default:
		return StatusCurrent
	}
}

// UsedBps es el porcentaje utilizado en puntos básicos, acotado a 10000.
func (c *CreditLine) UsedBps() int64 {
	if c.Limit <= 0 {
		return 0
	}
	used := new(big.Int).Mul(big.NewInt(int64(c.Balance())), big.NewInt(10_000))
	used.Quo(used, big.NewInt(int64(c.Limit)))
	if used.Cmp(big.NewInt(10_000)) > 0 {
		return 10_000
	}
	return used.Int64()
}

// Clone devuelve una copia profunda, para operar sin efectos si hay rechazo.
func (c *CreditLine) Clone() *CreditLine {
	cp := *c
	cp.Accrual = new(big.Int).Set(c.Accrual)
	return &cp
}

// Preview es la vista previa, sin efectos, de la liquidación a una fecha.
type Preview struct {
	Date                time.Time
	Capital             Money
	InterestDue         Money
	DaysSinceAnchor     int64
	InterestToLiquidate Money
	TotalBalance        Money
}

// PreviewAt calcula lo que se liquidaría a una fecha, sin modificar el crédito.
func (c *CreditLine) PreviewAt(date time.Time) (Preview, error) {
	date = truncateDate(date)
	if err := c.checkDate(date); err != nil {
		return Preview{}, err
	}
	return c.previewUnchecked(date)
}

// ProjectionAt es como PreviewAt pero tolera fechas anteriores al último
// movimiento (devengo 0). Se usa para "interés devengado a hoy".
func (c *CreditLine) ProjectionAt(date time.Time) (Preview, error) {
	date = truncateDate(date)
	if date.Before(c.LastAccrualDate) {
		date = c.LastAccrualDate
	}
	return c.previewUnchecked(date)
}

func (c *CreditLine) previewUnchecked(date time.Time) (Preview, error) {
	acc := AccrueTramo(c.Accrual, c.Capital, c.DailyRateE15, daysBetween(c.LastAccrualDate, date))
	interest, err := RoundAccrual(acc)
	if err != nil {
		return Preview{}, overflowError()
	}
	total, err := c.Balance().Add(interest)
	if err != nil {
		return Preview{}, overflowError()
	}
	return Preview{
		Date: date, Capital: c.Capital, InterestDue: c.InterestDue,
		DaysSinceAnchor: daysBetween(c.AnchorDate, date), InterestToLiquidate: interest, TotalBalance: total,
	}, nil
}

// Purchase registra un consumo contra el disponible.
func (c *CreditLine) Purchase(date time.Time, amount Money, desc string, newID NewIDFunc) (Entry, error) {
	date = truncateDate(date)
	if err := c.checkDate(date); err != nil {
		return Entry{}, err
	}
	if !amount.IsPositive() {
		return Entry{}, validationError("amountCents", "El monto debe ser mayor que cero.")
	}
	if amount > c.Available() {
		return Entry{}, newError(CodeInsufficientAvailable, "Fondos insuficientes.",
			map[string]any{"availableCents": c.Available()})
	}
	c.accrueTo(date)
	c.Capital += amount // no desborda: amount <= disponible <= cupo
	return c.appendEntry(Entry{ID: newID(), Type: EntryPurchase, Amount: amount, Date: date,
		Description: defaultDesc(desc, "Consumo")}), nil
}

// LiquidateInterest liquida el interés devengado hasta una fecha.
func (c *CreditLine) LiquidateInterest(date time.Time, newID NewIDFunc) (Entry, error) {
	date = truncateDate(date)
	if err := c.checkDate(date); err != nil {
		return Entry{}, err
	}
	p, err := c.previewUnchecked(date)
	if err != nil {
		return Entry{}, err
	}
	if p.InterestToLiquidate == 0 {
		return Entry{}, newError(CodeNothingToLiquidate, "No hay interés devengado para liquidar a esa fecha.", nil)
	}
	return c.liquidate(date, p, newID)
}

// Pay registra un pago: liquida el interés a la fecha, cubre primero el
// interés y aplica el resto a capital FIFO sobre openUses. Devuelve el
// movimiento INTERES automático (si hubo) y el PAGO.
func (c *CreditLine) Pay(date time.Time, amount Money, desc string, openUses []OpenUse, newID NewIDFunc) ([]Entry, error) {
	date = truncateDate(date)
	if err := c.checkDate(date); err != nil {
		return nil, err
	}
	if !amount.IsPositive() {
		return nil, validationError("amountCents", "El monto debe ser mayor que cero.")
	}
	p, err := c.previewUnchecked(date)
	if err != nil {
		return nil, err
	}
	if amount > p.TotalBalance {
		return nil, newError(CodePaymentExceedsBalance, "El pago supera el saldo pendiente.",
			map[string]any{"balanceCents": p.TotalBalance})
	}

	// Todo se calcula antes de mutar: un rechazo no deja efectos.
	toInterest := minMoney(amount, c.InterestDue+p.InterestToLiquidate)
	toCapital := amount - toInterest
	allocs, err := allocateFIFO(toCapital, openUses)
	if err != nil {
		return nil, err
	}

	var out []Entry
	if p.InterestToLiquidate > 0 {
		ie, err := c.liquidate(date, p, newID)
		if err != nil {
			return nil, err
		}
		out = append(out, ie)
	} else {
		c.accrueTo(date)
	}
	c.InterestDue -= toInterest
	c.Capital -= toCapital
	out = append(out, c.appendEntry(Entry{ID: newID(), Type: EntryPayment, Amount: amount, Date: date,
		Description: defaultDesc(desc, "Pago"), ToInterest: toInterest, Allocations: allocs}))
	return out, nil
}

func (c *CreditLine) liquidate(date time.Time, p Preview, newID NewIDFunc) (Entry, error) {
	due, err := c.InterestDue.Add(p.InterestToLiquidate)
	if err != nil {
		return Entry{}, overflowError()
	}
	c.InterestDue = due
	c.Accrual = new(big.Int) // la fracción se descarta: no se arrastra
	c.LastAccrualDate = date
	c.AnchorDate = date
	return c.appendEntry(Entry{ID: newID(), Type: EntryInterest, Amount: p.InterestToLiquidate, Date: date,
		Description: "Liquidación de interés",
		Detail:      interestDetail(p.DaysSinceAnchor, c.RateEABps)}), nil
}

func (c *CreditLine) accrueTo(date time.Time) {
	c.Accrual = AccrueTramo(c.Accrual, c.Capital, c.DailyRateE15, daysBetween(c.LastAccrualDate, date))
	if date.After(c.LastAccrualDate) {
		c.LastAccrualDate = date
	}
}

func (c *CreditLine) checkDate(date time.Time) error {
	if date.IsZero() {
		return validationError("date", "Indica la fecha del movimiento.")
	}
	if date.Before(c.LastEntryDate) {
		return newError(CodeDateBeforeLastEntry, "La fecha no puede ser anterior al último movimiento.",
			map[string]any{"lastEntryDate": c.LastEntryDate.Format(DateLayout)})
	}
	return nil
}

func (c *CreditLine) appendEntry(e Entry) Entry {
	c.LastSeq++
	c.LastEntryDate = e.Date
	e.Seq = c.LastSeq
	e.CreditLineID = c.ID
	e.BalanceAfter = c.Balance()
	return e
}

// DateLayout es el formato de fecha de la API (YYYY-MM-DD).
const DateLayout = "2006-01-02"

func truncateDate(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func daysBetween(a, b time.Time) int64 {
	if !b.After(a) {
		return 0
	}
	return int64(b.Sub(a).Hours() / 24)
}

func defaultDesc(desc, fallback string) string {
	if d := strings.TrimSpace(desc); d != "" {
		return d
	}
	return fallback
}

func interestDetail(days int64, bps int) string {
	return formatInt(days) + " días al " + formatBps(bps) + "% EA"
}
