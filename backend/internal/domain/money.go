// Package domain contiene las reglas del ledger de crédito. No depende de
// infraestructura: ni HTTP, ni SQL, ni el reloj del sistema.
package domain

import (
	"errors"
	"math"
)

// Money representa un monto en centavos de peso colombiano (COP).
// Nunca se usa punto flotante para dinero.
type Money int64

// ErrMoneyOverflow indica que una operación excede el rango de int64.
var ErrMoneyOverflow = errors.New("money: overflow")

// Add suma dos montos detectando desbordamiento.
func (m Money) Add(o Money) (Money, error) {
	if (o > 0 && m > math.MaxInt64-o) || (o < 0 && m < math.MinInt64-o) {
		return 0, ErrMoneyOverflow
	}
	return m + o, nil
}

// Sub resta dos montos detectando desbordamiento.
func (m Money) Sub(o Money) (Money, error) {
	if (o < 0 && m > math.MaxInt64+o) || (o > 0 && m < math.MinInt64+o) {
		return 0, ErrMoneyOverflow
	}
	return m - o, nil
}

// Cmp devuelve -1, 0 o 1 según m sea menor, igual o mayor que o.
func (m Money) Cmp(o Money) int {
	switch {
	case m < o:
		return -1
	case m > o:
		return 1
	default:
		return 0
	}
}

// IsPositive indica si el monto es estrictamente mayor que cero.
func (m Money) IsPositive() bool { return m > 0 }

func minMoney(a, b Money) Money {
	if a < b {
		return a
	}
	return b
}

func maxMoney(a, b Money) Money {
	if a > b {
		return a
	}
	return b
}
