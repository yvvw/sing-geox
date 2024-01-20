package http

import (
	"io"
	"net/http"
)

func GetBytes(url *string) ([]byte, error) {
	resp, err := http.Get(*url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
