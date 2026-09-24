package domain

import (
	"errors"
	"math"
	"testing"
)

func TestMoneyAdd(t *testing.T) {
	tests := []struct {
		name    string
		a, b    Money
		want    Money
		wantErr error
	}{
		{"positivos", 150000050, 50, 150000100, nil},
		{"con cero", 0, 0, 0, nil},
		{"negativo", 100, -300, -200, nil},
		{"overflow positivo", math.MaxInt64, 1, 0, ErrMoneyOverflow},
		{"overflow negativo", math.MinInt64, -1, 0, ErrMoneyOverflow},
		{"limite exacto", math.MaxInt64 - 1, 1, math.MaxInt64, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.a.Add(tt.b)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMoneySub(t *testing.T) {
	tests := []struct {
		name    string
		a, b    Money
		want    Money
		wantErr error
	}{
		{"simple", 500, 200, 300, nil},
		{"resultado negativo", 200, 500, -300, nil},
		{"con cero", 0, 0, 0, nil},
		{"overflow negativo", math.MinInt64, 1, 0, ErrMoneyOverflow},
		{"overflow positivo", math.MaxInt64, -1, 0, ErrMoneyOverflow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.a.Sub(tt.b)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMoneyCmpAndPositive(t *testing.T) {
	if Money(1).Cmp(2) != -1 || Money(2).Cmp(2) != 0 || Money(3).Cmp(2) != 1 {
		t.Fatal("Cmp incorrecto")
	}
	if Money(0).IsPositive() || Money(-1).IsPositive() || !Money(1).IsPositive() {
		t.Fatal("IsPositive incorrecto")
	}
}
