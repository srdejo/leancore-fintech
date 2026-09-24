package domain

import (
	"errors"
	"slices"
	"strconv"
)

// ErrInconsistentUses indica que los usos pendientes no cubren el capital a
// abonar: el saldo materializado y el historial divergen.
var ErrInconsistentUses = errors.New("allocation: usos pendientes insuficientes")

// allocateFIFO reparte toCapital entre los usos con capital pendiente, del
// más antiguo (menor seq) al más nuevo.
func allocateFIFO(toCapital Money, uses []OpenUse) ([]Allocation, error) {
	if toCapital <= 0 {
		return nil, nil
	}
	sorted := slices.Clone(uses)
	slices.SortFunc(sorted, func(a, b OpenUse) int { return a.Seq - b.Seq })

	remaining := toCapital
	var out []Allocation
	for _, u := range sorted {
		if remaining == 0 {
			break
		}
		if u.Pending <= 0 {
			continue
		}
		take := minMoney(remaining, u.Pending)
		out = append(out, Allocation{UseEntryID: u.EntryID, UseSeq: u.Seq, ToCapital: take})
		remaining -= take
	}
	if remaining != 0 {
		return nil, ErrInconsistentUses
	}
	return out, nil
}

func formatInt(n int64) string { return strconv.FormatInt(n, 10) }

// formatBps muestra puntos básicos como porcentaje con coma decimal (2450 -> "24,5").
func formatBps(bps int) string {
	whole, frac := bps/100, bps%100
	if frac == 0 {
		return strconv.Itoa(whole)
	}
	s := strconv.Itoa(frac)
	if frac < 10 {
		s = "0" + s
	}
	if s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	return strconv.Itoa(whole) + "," + s
}
