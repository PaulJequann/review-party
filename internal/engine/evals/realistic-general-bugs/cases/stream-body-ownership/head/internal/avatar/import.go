package avatar

import (
	"context"
	"io"

	"example.com/archive/internal/remote"
)

func Import(ctx context.Context, client remote.Client, url string, destination io.Writer) error {
	body, err := client.Open(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()
	_, err = io.Copy(destination, body)
	return err
}
