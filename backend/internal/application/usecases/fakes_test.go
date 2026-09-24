package usecases

import (
	"fmt"
	"sync"
	"time"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Today() time.Time { return c.t }

type seqIDs struct {
	mu sync.Mutex
	n  int
}

func (g *seqIDs) NewID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return fmt.Sprintf("id-%d", g.n)
}

func (g *seqIDs) NewAccountNumber() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return fmt.Sprintf("0000-%04d", g.n)
}
