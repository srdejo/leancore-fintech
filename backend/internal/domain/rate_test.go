package domain

import (
	"math"
	"testing"
)

func TestDailyRateE15(t *testing.T) {
	for _, bps := range []int{2400, 1, 2450, 3912, 10_000} {
		got, err := DailyRateE15(bps)
		if err != nil {
			t.Fatalf("bps %d: %v", bps, err)
		}
		// Referencia independiente con float64 (math.Pow): válida hasta ~1
		// unidad de 10^-15 por su precisión, suficiente para contrastar.
		want := (math.Pow(1+float64(bps)/10_000, 1.0/365) - 1) * 1e15
		if math.Abs(float64(got)-want) > 2 {
			t.Fatalf("bps %d: got %d, referencia %.2f", bps, got, want)
		}
	}
}

func TestDailyRateE15For24PercentIsAbout589e9(t *testing.T) {
	got, _ := DailyRateE15(2400)
	if got < 589_000_000_000 || got > 590_000_000_000 {
		t.Fatalf("24%% EA: got %d, esperado ~589e9", got)
	}
	// Al componer la tasa diaria 365 veces se recupera la EA (tolerancia 1e-9).
	ea := math.Pow(1+float64(got)/1e15, 365) - 1
	if math.Abs(ea-0.24) > 1e-9 {
		t.Fatalf("EA recompuesta %.12f", ea)
	}
}

func TestDailyRateE15Zero(t *testing.T) {
	got, err := DailyRateE15(0)
	if err != nil || got != 0 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestDailyRateE15OutOfRange(t *testing.T) {
	if _, err := DailyRateE15(-1); err == nil {
		t.Fatal("esperaba error para tasa negativa")
	}
	if _, err := DailyRateE15(MaxRateEABps + 1); err == nil {
		t.Fatal("esperaba error para tasa excesiva")
	}
}

func TestDailyRateE15IsDeterministic(t *testing.T) {
	a, _ := DailyRateE15(2400)
	b, _ := DailyRateE15(2400)
	if a != b {
		t.Fatal("no determinista")
	}
}
