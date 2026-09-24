package domain

import (
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse(DateLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

func idGen() NewIDFunc {
	n := 0
	return func() string { n++; return fmt.Sprintf("e%d", n) }
}

func wantCode(t *testing.T, err error, code Code) *Error {
	t.Helper()
	var de *Error
	if !errors.As(err, &de) || de.Code != code {
		t.Fatalf("err = %v, want code %s", err, code)
	}
	return de
}

func openLine(t *testing.T, limit, disb Money, bps int, date string) (*CreditLine, Entry) {
	t.Helper()
	cl, e, err := Open(OpenParams{ID: "c1", AccountNumber: "0042-7781", Holder: "María Fernanda Ruiz",
		Limit: limit, RateEABps: bps, DisbursementID: "e0", Disbursement: disb, DisbursementDate: d(date)})
	if err != nil {
		t.Fatal(err)
	}
	return cl, e
}

// accrualCents fija un devengo pendiente equivalente a cents centavos exactos.
func accrualCents(cents int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(cents), bigScale)
}

// --- 3.1 Apertura -----------------------------------------------------------

func TestOpenExitosa(t *testing.T) {
	cl, e := openLine(t, 1_000_000_000, 500_000_000, 2400, "2026-06-01")
	if cl.Capital != 500_000_000 || cl.InterestDue != 0 || cl.Available() != 500_000_000 {
		t.Fatalf("saldo inesperado: %+v", cl)
	}
	if cl.AccountNumber == "" || cl.DailyRateE15 == 0 {
		t.Fatal("faltan número de cuenta o tasa diaria congelada")
	}
	if e.Type != EntryDisbursement || e.Amount != 500_000_000 || !e.Date.Equal(d("2026-06-01")) || e.Seq != 1 {
		t.Fatalf("desembolso inesperado: %+v", e)
	}
	if e.Description != "Desembolso inicial del crédito" || e.BalanceAfter != 500_000_000 {
		t.Fatalf("descripción o saldo: %+v", e)
	}
}

func TestOpenCupoCompleto(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 1_000_000_000, 2400, "2026-06-01")
	if cl.Available() != 0 {
		t.Fatalf("disponible = %d", cl.Available())
	}
}

func TestOpenDesembolsoMayorAlCupo(t *testing.T) {
	_, _, err := Open(OpenParams{Holder: "X", Limit: 1_000_000_000, RateEABps: 2400,
		Disbursement: 1_000_000_001, DisbursementDate: d("2026-06-01")})
	wantCode(t, err, CodeDisbursementExceeds)
}

func TestOpenDatosInvalidos(t *testing.T) {
	base := OpenParams{Holder: "X", Limit: 100, RateEABps: 2400, Disbursement: 50, DisbursementDate: d("2026-06-01")}
	tests := []struct {
		field string
		mut   func(*OpenParams)
	}{
		{"holder", func(p *OpenParams) { p.Holder = "   " }},
		{"limitCents", func(p *OpenParams) { p.Limit = 0 }},
		{"amountCents", func(p *OpenParams) { p.Disbursement = 0 }},
		{"amountCents", func(p *OpenParams) { p.Disbursement = -5 }},
		{"date", func(p *OpenParams) { p.DisbursementDate = time.Time{} }},
		{"rateEaBps", func(p *OpenParams) { p.RateEABps = -1 }},
	}
	for _, tt := range tests {
		p := base
		tt.mut(&p)
		_, _, err := Open(p)
		de := wantCode(t, err, CodeValidation)
		if de.Details["field"] != tt.field {
			t.Fatalf("field = %v, want %s", de.Details["field"], tt.field)
		}
	}
}

// --- 3.2 Fechas, disponible y estado ---------------------------------------

func TestFechaAnteriorRechazada(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 100, 2400, "2026-08-05")
	_, err := cl.Purchase(d("2026-08-01"), 100, "", idGen())
	de := wantCode(t, err, CodeDateBeforeLastEntry)
	if de.Details["lastEntryDate"] != "2026-08-05" {
		t.Fatalf("details = %v", de.Details)
	}
}

func TestMismaFechaPermitida(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 100, 2400, "2026-08-05")
	if _, err := cl.Purchase(d("2026-08-05"), 100, "", idGen()); err != nil {
		t.Fatal(err)
	}
}

func TestEstados(t *testing.T) {
	cl := &CreditLine{Limit: 1_000_000_000, Capital: 615_000_000, InterestDue: 6_438_000}
	if cl.Status() != StatusCurrent {
		t.Fatalf("got %s", cl.Status())
	}
	cl = &CreditLine{Limit: 500_000_000, Capital: 500_000_000, InterestDue: 1_240_000}
	if cl.Status() != StatusOverdrawn || cl.Available() != 0 || cl.UsedBps() != 10_000 {
		t.Fatalf("sobregiro: %s %d %d", cl.Status(), cl.Available(), cl.UsedBps())
	}
	cl = &CreditLine{Limit: 1_000}
	if cl.Status() != StatusNoBalance {
		t.Fatalf("got %s", cl.Status())
	}
	cl = &CreditLine{Limit: 1_000_000_000, Capital: 621_438_000}
	if cl.UsedBps() != 6214 {
		t.Fatalf("usedBps = %d", cl.UsedBps())
	}
}

// --- 3.3 Consumos -----------------------------------------------------------

func TestConsumoDentroDelDisponible(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 400_000_000, 2400, "2026-06-01")
	e, err := cl.Purchase(d("2026-06-01"), 35_000_000, "Compra supermercado", idGen())
	if err != nil {
		t.Fatal(err)
	}
	if cl.Capital != 435_000_000 || cl.Available() != 565_000_000 {
		t.Fatalf("capital %d disponible %d", cl.Capital, cl.Available())
	}
	if e.Type != EntryPurchase || e.Seq != 2 || e.BalanceAfter != 435_000_000 || e.Description != "Compra supermercado" {
		t.Fatalf("entry %+v", e)
	}
}

func TestConsumoMayorAlDisponible(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 975_000_000, 2400, "2026-06-01")
	_, err := cl.Purchase(d("2026-06-02"), 30_000_000, "", idGen())
	de := wantCode(t, err, CodeInsufficientAvailable)
	if de.Details["availableCents"] != Money(25_000_000) {
		t.Fatalf("details %v", de.Details)
	}
	if cl.Capital != 975_000_000 || cl.LastSeq != 1 {
		t.Fatal("el rechazo dejó efectos")
	}
}

func TestConsumoMontoNoPositivo(t *testing.T) {
	cl, _ := openLine(t, 1_000, 100, 2400, "2026-06-01")
	for _, amt := range []Money{0, -1} {
		_, err := cl.Purchase(d("2026-06-01"), amt, "", idGen())
		wantCode(t, err, CodeValidation)
	}
}

func TestConsumoDevengaElTramoAntesDeCambiarCapital(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 400_000_000, 2400, "2026-06-01")
	if _, err := cl.Purchase(d("2026-06-05"), 350_000_000, "", idGen()); err != nil {
		t.Fatal(err)
	}
	want := AccrueTramo(new(big.Int), 400_000_000, cl.DailyRateE15, 4)
	if cl.Accrual.Cmp(want) != 0 || !cl.LastAccrualDate.Equal(d("2026-06-05")) {
		t.Fatalf("accrual %s want %s", cl.Accrual, want)
	}
}

// --- 3.4 Liquidación y vista previa ------------------------------------------

func TestVistaPreviaSinEfectos(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 400_000_000, 2400, "2026-06-01")
	before := cl.Clone()
	p, err := cl.PreviewAt(d("2026-07-01"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := RoundAccrual(AccrueTramo(new(big.Int), 400_000_000, cl.DailyRateE15, 30))
	if p.Capital != 400_000_000 || p.DaysSinceAnchor != 30 || p.InterestToLiquidate != want || want == 0 {
		t.Fatalf("preview %+v (want interés %d)", p, want)
	}
	if p.TotalBalance != 400_000_000+want {
		t.Fatalf("total %d", p.TotalBalance)
	}
	if !reflect.DeepEqual(before, cl) {
		t.Fatal("la vista previa modificó el crédito")
	}
}

func TestLiquidacionExitosa(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 400_000_000, 2400, "2026-06-01")
	p, _ := cl.PreviewAt(d("2026-07-01"))
	e, err := cl.LiquidateInterest(d("2026-07-01"), idGen())
	if err != nil {
		t.Fatal(err)
	}
	if e.Type != EntryInterest || e.Amount != p.InterestToLiquidate || cl.InterestDue != p.InterestToLiquidate {
		t.Fatalf("entry %+v due %d", e, cl.InterestDue)
	}
	if cl.Accrual.Sign() != 0 || !cl.AnchorDate.Equal(d("2026-07-01")) {
		t.Fatal("no reinició el devengo")
	}
	if e.Detail != "30 días al 24% EA" {
		t.Fatalf("detalle %q", e.Detail)
	}
}

func TestNadaQueLiquidar(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 400_000_000, 2400, "2026-06-01")
	if _, err := cl.LiquidateInterest(d("2026-07-01"), idGen()); err != nil {
		t.Fatal(err)
	}
	_, err := cl.LiquidateInterest(d("2026-07-01"), idGen())
	wantCode(t, err, CodeNothingToLiquidate)

	zero, _ := openLine(t, 1_000, 100, 0, "2026-06-01")
	_, err = zero.LiquidateInterest(d("2026-12-01"), idGen())
	wantCode(t, err, CodeNothingToLiquidate)
}

func TestInteresQueSobregira(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 999_500_000, 2400, "2026-06-01")
	cl.Accrual = accrualCents(800_000)
	if _, err := cl.LiquidateInterest(d("2026-06-01"), idGen()); err != nil {
		t.Fatal(err)
	}
	if cl.Balance() != 1_000_300_000 || cl.Available() != 0 || cl.Status() != StatusOverdrawn {
		t.Fatalf("saldo %d disp %d estado %s", cl.Balance(), cl.Available(), cl.Status())
	}
	_, err := cl.Purchase(d("2026-06-01"), 1_000, "", idGen())
	wantCode(t, err, CodeInsufficientAvailable)

	if _, err := cl.Pay(d("2026-06-01"), 5_000_000, "", []OpenUse{{EntryID: "e0", Seq: 1, Pending: 999_500_000}}, idGen()); err != nil {
		t.Fatal(err)
	}
	if cl.Balance() != 995_300_000 || cl.Available() != 4_700_000 || cl.Status() == StatusOverdrawn {
		t.Fatalf("saldo %d disp %d estado %s", cl.Balance(), cl.Available(), cl.Status())
	}
}

// --- 3.5 Pagos y FIFO -------------------------------------------------------

func TestPagoCubreInteresYCapital(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 580_000_000, 2400, "2026-06-01")
	cl.Accrual = accrualCents(11_000_000)
	entries, err := cl.Pay(d("2026-06-01"), 120_000_000, "", []OpenUse{{EntryID: "e0", Seq: 1, Pending: 580_000_000}}, idGen())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Type != EntryInterest || entries[0].Amount != 11_000_000 {
		t.Fatalf("esperaba INTERES y PAGO: %+v", entries)
	}
	pay := entries[1]
	if pay.Type != EntryPayment || pay.ToInterest != 11_000_000 || cl.Capital != 471_000_000 || cl.InterestDue != 0 {
		t.Fatalf("pago %+v capital %d", pay, cl.Capital)
	}
	if len(pay.Allocations) != 1 || pay.Allocations[0].ToCapital != 109_000_000 {
		t.Fatalf("asignaciones %+v", pay.Allocations)
	}
	if entries[0].Seq != 2 || pay.Seq != 3 || pay.BalanceAfter != 471_000_000 {
		t.Fatalf("seq/saldo %+v", entries)
	}
}

func TestPagoSoloAlcanzaParaInteres(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 100_000_000, 2400, "2026-06-01")
	cl.InterestDue = 5_000_000
	entries, err := cl.Pay(d("2026-06-01"), 3_000_000, "", nil, idGen())
	if err != nil {
		t.Fatal(err)
	}
	if cl.InterestDue != 2_000_000 || cl.Capital != 100_000_000 || entries[0].ToInterest != 3_000_000 || entries[0].Allocations != nil {
		t.Fatalf("due %d capital %d %+v", cl.InterestDue, cl.Capital, entries)
	}
}

func TestPagoSaldoTotal(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 400_000_000, 2400, "2026-06-01")
	p, _ := cl.PreviewAt(d("2026-07-15"))
	if _, err := cl.Pay(d("2026-07-15"), p.TotalBalance, "", []OpenUse{{EntryID: "e0", Seq: 1, Pending: 400_000_000}}, idGen()); err != nil {
		t.Fatal(err)
	}
	if cl.Balance() != 0 || cl.Status() != StatusNoBalance {
		t.Fatalf("saldo %d", cl.Balance())
	}
}

func TestPagoMayorAlSaldoNoDejaEfectos(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 471_000_000, 2400, "2026-06-01")
	cl.Accrual = accrualCents(0)
	before := cl.Clone()
	_, err := cl.Pay(d("2026-06-01"), 471_000_001, "", []OpenUse{{EntryID: "e0", Seq: 1, Pending: 471_000_000}}, idGen())
	de := wantCode(t, err, CodePaymentExceedsBalance)
	if de.Details["balanceCents"] != Money(471_000_000) {
		t.Fatalf("details %v", de.Details)
	}
	// Con interés pendiente, el rechazo tampoco genera el INTERES automático.
	cl2, _ := openLine(t, 1_000_000_000, 100_000_000, 2400, "2026-06-01")
	before2 := cl2.Clone()
	_, err = cl2.Pay(d("2026-09-01"), 900_000_000, "", nil, idGen())
	wantCode(t, err, CodePaymentExceedsBalance)
	if !reflect.DeepEqual(before, cl) || !reflect.DeepEqual(before2, cl2) {
		t.Fatal("el rechazo dejó efectos")
	}
}

func TestPagoMontoNoPositivo(t *testing.T) {
	cl, _ := openLine(t, 1_000, 100, 2400, "2026-06-01")
	_, err := cl.Pay(d("2026-06-01"), 0, "", nil, idGen())
	wantCode(t, err, CodeValidation)
}

func TestFIFOCierraUnUsoYAbonaElSiguiente(t *testing.T) {
	cl := &CreditLine{ID: "c1", Limit: 1_000_000_000, Capital: 85_000_000, Accrual: new(big.Int),
		LastAccrualDate: d("2026-06-10"), AnchorDate: d("2026-06-10"), LastEntryDate: d("2026-06-10"), LastSeq: 3}
	uses := []OpenUse{{"u3", 3, 10_000_000}, {"u1", 1, 40_000_000}, {"u2", 2, 35_000_000}}
	entries, err := cl.Pay(d("2026-06-10"), 50_000_000, "", uses, idGen())
	if err != nil {
		t.Fatal(err)
	}
	want := []Allocation{{"u1", 1, 40_000_000}, {"u2", 2, 10_000_000}}
	if !reflect.DeepEqual(entries[0].Allocations, want) {
		t.Fatalf("got %+v", entries[0].Allocations)
	}
}

func TestPagoTrasLiquidacionManualDelMismoDia(t *testing.T) {
	cl, _ := openLine(t, 1_000_000_000, 400_000_000, 2400, "2026-06-01")
	if _, err := cl.LiquidateInterest(d("2026-09-01"), idGen()); err != nil {
		t.Fatal(err)
	}
	entries, err := cl.Pay(d("2026-09-01"), 1_000_000, "", []OpenUse{{"e0", 1, 400_000_000}}, idGen())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Type != EntryPayment {
		t.Fatalf("no debía generar INTERES: %+v", entries)
	}
}

func TestUsosInconsistentes(t *testing.T) {
	cl, _ := openLine(t, 1_000, 500, 0, "2026-06-01")
	_, err := cl.Pay(d("2026-06-01"), 500, "", []OpenUse{{"e0", 1, 100}}, idGen())
	if !errors.Is(err, ErrInconsistentUses) {
		t.Fatalf("err = %v", err)
	}
}
