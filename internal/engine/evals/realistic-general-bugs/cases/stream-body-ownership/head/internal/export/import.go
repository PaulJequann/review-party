package export

import (
	"context"
	"crypto/sha256"
	"io"

	"example.com/archive/internal/remote"
)

func Digest(ctx context.Context, client remote.Client, url string) ([32]byte, error) {
	body, err := client.Open(ctx, url)
	if err != nil {
		return [32]byte{}, err
	}
	defer body.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, body); err != nil {
		return [32]byte{}, err
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}
