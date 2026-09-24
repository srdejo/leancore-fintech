package domain

import (
	"errors"
	"math"
	"math/big"
)

const (
	// DailyRateScale es la escala de la tasa diaria almacenada (10^15).
	DailyRateScale int64 = 1_000_000_000_000_000
	daysPerYear          = 365
	ratePrecision        = 256
	// MaxRateEABps limita la tasa a valores razonables (1000% EA) para
	// mantener acotados los cálculos.
	MaxRateEABps = 100_000
)

// ErrInvalidRate indica una tasa fuera de rango.
var ErrInvalidRate = errors.New("rate: fuera de rango")

// DailyRateE15 deriva la tasa diaria equivalente a una tasa efectiva anual:
// round_half_up(((1 + EA)^(1/365) - 1) × 10^15).
//
// Es la única operación no entera del dominio: se aplica a una constante de
// tasa, una sola vez al abrir el crédito, con 256 bits de precisión. El
// resultado se almacena y nunca se recalcula.
func DailyRateE15(rateEABps int) (int64, error) {
	if rateEABps < 0 || rateEABps > MaxRateEABps {
		return 0, ErrInvalidRate
	}
	if rateEABps == 0 {
		return 0, nil
	}
	newF := func() *big.Float { return new(big.Float).SetPrec(ratePrecision) }

	// a = 1 + bps/10000
	a := newF().Quo(newF().SetInt64(int64(rateEABps)), newF().SetInt64(10_000))
	a.Add(a, newF().SetInt64(1))

	// Semilla float64 cercana a la raíz; Newton la refina a 256 bits (la
	// precisión final no depende de la semilla).
	seed := math.Pow(1+float64(rateEABps)/10_000, 1.0/daysPerYear)
	root := nthRoot(a, daysPerYear, newF().SetFloat64(seed), newF)

	// (root - 1) × 10^15 + 0.5, truncado => HALF_UP para valores positivos.
	root.Sub(root, newF().SetInt64(1))
	root.Mul(root, newF().SetInt64(DailyRateScale))
	root.Add(root, newF().SetFloat64(0.5))
	scaled, _ := root.Int(nil)
	return scaled.Int64(), nil
}

// nthRoot calcula a^(1/n) con el método de Newton:
// x' = ((n-1)·x + a / x^(n-1)) / n.
func nthRoot(a *big.Float, n int64, seed *big.Float, newF func() *big.Float) *big.Float {
	x := seed
	nMinus1 := newF().SetInt64(n - 1)
	nF := newF().SetInt64(n)
	// Convergencia cuadrática desde una semilla de ~16 dígitos: bastan unas
	// pocas iteraciones para 256 bits; 20 es un margen amplio.
	for range 20 {
		pow := powInt(x, n-1, newF)
		next := newF().Quo(a, pow)
		next.Add(next, newF().Mul(nMinus1, x))
		next.Quo(next, nF)
		if next.Cmp(x) == 0 {
			break
		}
		x = next
	}
	return x
}

func powInt(base *big.Float, exp int64, newF func() *big.Float) *big.Float {
	result := newF().SetInt64(1)
	b := newF().Set(base)
	for exp > 0 {
		if exp&1 == 1 {
			result.Mul(result, b)
		}
		b.Mul(b, b)
		exp >>= 1
	}
	return result
}
