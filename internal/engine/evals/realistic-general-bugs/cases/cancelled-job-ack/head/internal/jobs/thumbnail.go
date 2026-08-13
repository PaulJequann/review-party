package jobs

import (
	"context"
	"errors"
	"fmt"
)

type Renderer interface {
	Render(context.Context) ([]byte, error)
}

type OutputStore interface {
	Put(context.Context, []byte) error
}

type ThumbnailHandler struct {
	Renderer Renderer
	Store    OutputStore
}

func (handler ThumbnailHandler) Handle(ctx context.Context) error {
	thumbnail, err := handler.Renderer.Render(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("render thumbnail: %w", err)
	}
	if err := handler.Store.Put(ctx, thumbnail); err != nil {
		return fmt.Errorf("store thumbnail: %w", err)
	}
	return nil
}
