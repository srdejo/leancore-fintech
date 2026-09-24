package domain

import "testing"

// Escenario del prototipo "Ledger Crédito" (24% EA), del 2026-06-01 al 2026-09-01.
// Los valores esperados se calcularon por separado con big.Rat:
//
//	r = round_half_up((1,24^(1/365) - 1) × 10^15) = 589519944146
//	07-01 interés = HALF_UP((500000000×14 + 580000000×16) × r / 10^15) = 9597385
//	08-01 interés = HALF_UP(504597385×12 × r / 10^15)                  = 8829546
//	08-05 interés = HALF_UP(504597385×4  × r / 10^15)                  = 1189881 (automático al pagar)
//	09-01 interés = HALF_UP((424616812×17 + 549616812×10) × r / 10^15) = 7495542
func TestEscenarioCompletoDelPrototipo(t *testing.T) {
	cl, disb := openLine(t, 1_000_000_000, 500_000_000, 2400, "2026-06-01")
	if cl.DailyRateE15 != 589_519_944_146 {
		t.Fatalf("tasa diaria = %d", cl.DailyRateE15)
	}
	ids := idGen()
	pending := map[int]Money{1: 500_000_000}
	ids2 := map[int]string{1: disb.ID}
	uses := func() []OpenUse {
		var out []OpenUse
		for seq, p := range pending {
			out = append(out, OpenUse{EntryID: ids2[seq], Seq: seq, Pending: p})
		}
		return out
	}
	apply := func(es []Entry) {
		for _, e := range es {
			for _, a := range e.Allocations {
				pending[a.UseSeq] -= a.ToCapital
			}
		}
	}
	check := func(step string, e Entry, typ EntryType, amount, capital, due Money) {
		t.Helper()
		if e.Type != typ || e.Amount != amount || cl.Capital != capital || cl.InterestDue != due || e.BalanceAfter != capital+due {
			t.Fatalf("%s: entry %s %d saldo %d | capital %d due %d; want %s %d capital %d due %d",
				step, e.Type, e.Amount, e.BalanceAfter, cl.Capital, cl.InterestDue, typ, amount, capital, due)
		}
	}
	purchase := func(date string, amt Money) Entry {
		t.Helper()
		e, err := cl.Purchase(d(date), amt, "", ids)
		if err != nil {
			t.Fatal(err)
		}
		pending[e.Seq], ids2[e.Seq] = amt, e.ID
		return e
	}
	pay := func(date string, amt Money) []Entry {
		t.Helper()
		es, err := cl.Pay(d(date), amt, "", uses(), ids)
		if err != nil {
			t.Fatal(err)
		}
		apply(es)
		return es
	}
	liquidate := func(date string) Entry {
		t.Helper()
		e, err := cl.LiquidateInterest(d(date), ids)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}

	check("06-01 desembolso", disb, EntryDisbursement, 500_000_000, 500_000_000, 0)
	check("06-15 consumo", purchase("2026-06-15", 80_000_000), EntryPurchase, 80_000_000, 580_000_000, 0)
	check("07-01 interés", liquidate("2026-07-01"), EntryInterest, 9_597_385, 580_000_000, 9_597_385)

	es := pay("2026-07-01", 120_000_000)
	if len(es) != 1 || es[0].ToInterest != 9_597_385 {
		t.Fatalf("07-01 pago: %+v", es)
	}
	check("07-01 pago", es[0], EntryPayment, 120_000_000, 469_597_385, 0)

	check("07-20 consumo", purchase("2026-07-20", 35_000_000), EntryPurchase, 35_000_000, 504_597_385, 0)
	check("08-01 interés", liquidate("2026-08-01"), EntryInterest, 8_829_546, 504_597_385, 8_829_546)

	es = pay("2026-08-05", 90_000_000)
	if len(es) != 2 || es[0].Type != EntryInterest || es[0].Amount != 1_189_881 || es[1].ToInterest != 10_019_427 {
		t.Fatalf("08-05: %+v", es)
	}
	check("08-05 pago", es[1], EntryPayment, 90_000_000, 424_616_812, 0)

	check("08-22 consumo", purchase("2026-08-22", 125_000_000), EntryPurchase, 125_000_000, 549_616_812, 0)
	check("09-01 interés", liquidate("2026-09-01"), EntryInterest, 7_495_542, 549_616_812, 7_495_542)

	// FIFO: los dos abonos a capital (110402615 y 79980573) recaen en el desembolso.
	if pending[1] != 309_616_812 || pending[2] != 80_000_000 || pending[5] != 35_000_000 {
		t.Fatalf("pendientes por uso: %v", pending)
	}
	if cl.Balance() != 557_112_354 || cl.Available() != 442_887_646 || cl.Status() != StatusCurrent {
		t.Fatalf("final: saldo %d disp %d %s", cl.Balance(), cl.Available(), cl.Status())
	}
}
