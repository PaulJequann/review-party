package discovery

import (
	"context"
	"sync"
)

type cacheWriteTracker struct {
	mu      sync.Mutex
	pending int
	done    chan struct{}
}

func (tracker *cacheWriteTracker) start() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.pending == 0 {
		tracker.done = make(chan struct{})
	}
	tracker.pending++
}

func (tracker *cacheWriteTracker) finish() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.pending--
	if tracker.pending == 0 {
		close(tracker.done)
	}
}

func (tracker *cacheWriteTracker) wait(ctx context.Context) error {
	tracker.mu.Lock()
	done := tracker.done
	tracker.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
