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

func TestThumbnailCancellationRemainsRetryable(t *testing.T) {
	handler := ThumbnailHandler{Renderer: canceledRenderer{}, Store: unusedStore{}}
	if !errors.Is(handler.Handle(context.Background()), context.Canceled) {
		t.Fatal("cancellation was not returned")
	}
}
