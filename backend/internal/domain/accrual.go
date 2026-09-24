package domain

import (
	"errors"
	"math/big"
)

var bigScale = big.NewInt(DailyRateScale)

// ErrAccrualOverflow indica que el interés liquidado no cabe en Money.
var ErrAccrualOverflow = errors.New("accrual: overflow")

// AccrueTramo devuelve acc + capital × dailyRateE15 × días, en unidades
// enteras exactas (centavos × 10^15). No redondea.
func AccrueTramo(acc *big.Int, capital Money, dailyRateE15 int64, days int64) *big.Int {
	out := new(big.Int).Set(acc)
	if capital <= 0 || dailyRateE15 <= 0 || days <= 0 {
		return out
	}
	tramo := new(big.Int).Mul(big.NewInt(int64(capital)), big.NewInt(dailyRateE15))
	tramo.Mul(tramo, big.NewInt(days))
	return out.Add(out, tramo)
}

// RoundAccrual convierte unidades de devengo a centavos con HALF_UP:
// una fracción de 0,5 centavos o más sube un centavo; la menor se descarta.
func RoundAccrual(acc *big.Int) (Money, error) {
	if acc.Sign() <= 0 {
		return 0, nil
	}
	q, r := new(big.Int).QuoRem(acc, bigScale, new(big.Int))
	if new(big.Int).Lsh(r, 1).Cmp(bigScale) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return 0, ErrAccrualOverflow
	}
	return Money(q.Int64()), nil
}
