package remote

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

type Client struct {
	HTTP *http.Client
}

// Open returns a streaming response body. The caller owns and must close it.
func (client Client) Open(ctx context.Context, url string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, fmt.Errorf("remote status %d", response.StatusCode)
	}
	return response.Body, nil
}
