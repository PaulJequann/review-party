package fixture

import (
	"io"
	"net/http"
)

func Fetch(client *http.Client, url string) ([]byte, error) {
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(response.Body)
}
