package jobs

import (
	"context"
	"errors"
	"testing"
)

type canceledRenderer struct{}

func (canceledRenderer) Render(context.Context) ([]byte, error) {
	return nil, context.Canceled
}

type unusedStore struct{}

func (unusedStore) Put(context.Context, []byte) error {
	return errors.New("store should not be called")
}

func TestThumbnailCancellationStopsWork(t *testing.T) {
	handler := ThumbnailHandler{Renderer: canceledRenderer{}, Store: unusedStore{}}
	if err := handler.Handle(context.Background()); err != nil {
		t.Fatalf("cancellation should stop work quietly: %v", err)
	}
}
