package export

import (
	"context"
	"crypto/sha256"

	"example.com/archive/internal/remote"
)

func Digest(ctx context.Context, client remote.Client, url string) ([32]byte, error) {
	payload, err := client.Fetch(ctx, url)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(payload), nil
}
