package appstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

func (t *appstore) downloadArtwork(ctx context.Context, url string) ([]byte, error) {
	if url == "" {
		return nil, nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	req, err := t.httpClient.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	res, err := t.httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected artwork response status: %d", res.StatusCode)
	}

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read artwork: %w", err)
	}

	if len(data) == 0 {
		return nil, errors.New("artwork response is empty")
	}

	return data, nil
}
