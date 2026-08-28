package fixture

import (
	"context"
	"sync"
)

type Lock struct {
	mu   sync.Mutex
	tail chan struct{}
}

func (l *Lock) Wait(ctx context.Context) error {
	l.mu.Lock()
	tail := l.tail
	l.mu.Unlock()
	select {
	case <-tail:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
