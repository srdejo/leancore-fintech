// Package ids implementa ports.IDGenerator con UUID v4 y números de cuenta
// aleatorios con formato NNNN-NNNN.
package ids

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/google/uuid"
)

type Generator struct{}

func (Generator) NewID() string { return uuid.NewString() }

func (Generator) NewAccountNumber() string {
	return fmt.Sprintf("%04d-%04d", randN(10_000), randN(10_000))
}

func randN(n int64) int64 {
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		panic(err) // crypto/rand no falla en plataformas soportadas
	}
	return v.Int64()
}
