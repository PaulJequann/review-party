package avatar

import (
	"context"
	"io"

	"example.com/archive/internal/remote"
)

func Import(ctx context.Context, client remote.Client, url string, destination io.Writer) error {
	payload, err := client.Fetch(ctx, url)
	if err != nil {
		return err
	}
	_, err = destination.Write(payload)
	return err
}
