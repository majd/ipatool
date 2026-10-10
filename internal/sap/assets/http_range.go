package assets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const remoteReadAttempts = 3

type remoteFile struct {
	ctx    context.Context
	client *http.Client
	url    string
	size   int64
}

//nolint:wsl // Request construction and validation are clearer in protocol order.
func openRemoteFile(ctx context.Context, client *http.Client, source string) (*remoteFile, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, fmt.Errorf("create remote image request: %w", err)
	}

	request.Header.Set("Range", "bytes=0-0")
	response, err := client.Do(request)

	if err != nil {
		return nil, fmt.Errorf("open remote image: %w", err)
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("open remote image: server returned %s without byte-range support", response.Status)
	}

	start, end, size, err := parseContentRange(response.Header.Get("Content-Range"))
	if err != nil {
		return nil, fmt.Errorf("open remote image: %w", err)
	}
	if start != 0 || end != 0 {
		return nil, fmt.Errorf("open remote image: server returned unexpected byte range %d-%d", start, end)
	}

	return &remoteFile{ctx: ctx, client: client, url: source, size: size}, nil
}

//nolint:wsl // Parsing checks are clearer in wire-format order.
func parseContentRange(value string) (int64, int64, int64, error) {
	unitAndRange, total, ok := strings.Cut(value, "/")
	if !ok || total == "" || total == "*" {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range %q", value)
	}
	size, err := strconv.ParseInt(total, 10, 64)
	if err != nil || size <= 0 {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range %q", value)
	}
	rangeValue, ok := strings.CutPrefix(unitAndRange, "bytes ")
	if !ok {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range %q", value)
	}

	startValue, endValue, ok := strings.Cut(rangeValue, "-")
	if !ok {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range %q", value)
	}
	start, startErr := strconv.ParseInt(startValue, 10, 64)
	end, endErr := strconv.ParseInt(endValue, 10, 64)
	if startErr != nil || endErr != nil || start < 0 || end < start || end >= size {
		return 0, 0, 0, fmt.Errorf("invalid Content-Range %q", value)
	}

	return start, end, size, nil
}

//nolint:wsl // Range request validation is clearer in protocol order.
func (r *remoteFile) ReadAt(data []byte, offset int64) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}

	if offset < 0 {
		return 0, errors.New("negative remote image offset")
	}

	if offset >= r.size {
		return 0, io.EOF
	}

	wanted := len(data)
	if remaining := r.size - offset; int64(wanted) > remaining {
		wanted = int(remaining)
	}

	total := 0

	var lastErr error

	for attempt := 0; attempt < remoteReadAttempts && total < wanted; attempt++ {
		start := offset + int64(total)
		end := offset + int64(wanted) - 1

		request, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.url, nil)
		if err != nil {
			return total, fmt.Errorf("create remote range request: %w", err)
		}

		request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
		response, err := r.client.Do(request)
		if err != nil {
			lastErr = err

			continue
		}

		if response.StatusCode != http.StatusPartialContent {
			response.Body.Close()

			return total, fmt.Errorf("read remote image range: server returned %s", response.Status)
		}
		actualStart, actualEnd, actualSize, err := parseContentRange(response.Header.Get("Content-Range"))

		if err != nil || actualStart != start || actualEnd != end || actualSize != r.size {
			response.Body.Close()
			if err != nil {
				return total, fmt.Errorf("read remote image range: %w", err)
			}

			return total, fmt.Errorf("read remote image range: server returned bytes %d-%d/%d, expected %d-%d/%d", actualStart, actualEnd, actualSize, start, end, r.size)
		}

		count, readErr := io.ReadFull(response.Body, data[total:wanted])
		closeErr := response.Body.Close()
		total += count

		if readErr == nil {
			lastErr = closeErr

			break
		}

		lastErr = readErr
	}

	if total != wanted {
		if lastErr == nil {
			lastErr = io.ErrUnexpectedEOF
		}

		return total, fmt.Errorf("read remote image at offset %d: %w", offset+int64(total), lastErr)
	}

	if wanted != len(data) {
		return total, io.EOF
	}

	return total, nil
}
