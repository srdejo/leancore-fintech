package domain

import (
	"math/big"
	"testing"
)

// units construye un devengo equivalente a cents + num/den centavos.
func units(cents int64, frac int64) *big.Int {
	// frac expresado en unidades de 10^15 (p. ej. 0,23 centavos = 23 × 10^13).
	u := new(big.Int).Mul(big.NewInt(cents), bigScale)
	return u.Add(u, big.NewInt(frac))
}

func TestAccrueTramoPorTramos(t *testing.T) {
	r, _ := DailyRateE15(2400)
	acc := AccrueTramo(new(big.Int), 40_000_000, r, 4)
	acc = AccrueTramo(acc, 75_000_000, r, 5)

	want := new(big.Int).Mul(big.NewInt(40_000_000*4+75_000_000*5), big.NewInt(r))
	if acc.Cmp(want) != 0 {
		t.Fatalf("got %s, want %s", acc, want)
	}
}

func TestAccrueTramoSinCapitalNoDevenga(t *testing.T) {
	// El interés por pagar no genera interés: solo se pasa el capital (0).
	r, _ := DailyRateE15(2400)
	acc := AccrueTramo(new(big.Int), 0, r, 30)
	if acc.Sign() != 0 {
		t.Fatalf("got %s", acc)
	}
}

func TestAccrueTramoNoMutaElAcumulado(t *testing.T) {
	acc := big.NewInt(5)
	_ = AccrueTramo(acc, 100, 100, 1)
	if acc.Int64() != 5 {
		t.Fatal("mutó el acumulado de entrada")
	}
}

func TestRoundAccrualHalfUp(t *testing.T) {
	tests := []struct {
		name string
		acc  *big.Int
		want Money
	}{
		{",23 centavos se descarta", units(328764, 230_000_000_000_000), 328764},
		{",50 centavos sube", units(328764, 500_000_000_000_000), 328765},
		{",4999... se descarta", units(328764, 499_999_999_999_999), 328764},
		{",99 sube", units(328764, 990_000_000_000_000), 328765},
		{"exacto", units(1000, 0), 1000},
		{"cero", new(big.Int), 0},
		{"solo fracción menor a medio centavo", units(0, 400_000_000_000_000), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RoundAccrual(tt.acc)
			if err != nil || got != tt.want {
				t.Fatalf("got %d, %v; want %d", got, err, tt.want)
			}
		})
	}
}

func TestLiquidacionesSucesivasNoArrastranResiduo(t *testing.T) {
	// Primera liquidación descarta 0,4 centavos; el acumulado se reinicia.
	first, _ := RoundAccrual(units(10, 400_000_000_000_000))
	if first != 10 {
		t.Fatalf("primera = %d", first)
	}
	acc := new(big.Int) // reinicio tras liquidar
	acc.Add(acc, units(1000, 0))
	second, _ := RoundAccrual(acc)
	if second != 1000 {
		t.Fatalf("segunda = %d, want 1000", second)
	}
}
