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

func (client Client) Fetch(ctx context.Context, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.HTTP.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote status %d", response.StatusCode)
	}
	return io.ReadAll(response.Body)
}
