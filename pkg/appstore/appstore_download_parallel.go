package appstore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/schollz/progressbar/v3"
)

const (
	parallelDownloadThreshold = 128 << 20
	parallelDownloadCount     = 4
)

type parallelDownloadError struct {
	err       error
	validator string
}

func (e *parallelDownloadError) Error() string {
	return fmt.Sprintf("parallel download failed: %s", e.err)
}

func (e *parallelDownloadError) Unwrap() error {
	return e.err
}

func downloadPartsPath(destination string) string {
	return destination + ".ipatool-parts"
}

func downloadValidator(response *http.Response) string {
	etag := response.Header.Get("ETag")
	if strings.HasPrefix(etag, `"`) && strings.HasSuffix(etag, `"`) {
		return etag
	}

	return ""
}

// Keep out-of-order bytes out of the main file, so even an unclean process exit
// leaves its length describing a valid contiguous prefix for the next attempt.
//
//nolint:nonamedreturns // Staging cleanup errors must propagate to callers.
func (t *appstore) downloadFileParallel(
	ctx context.Context,
	cancel context.CancelFunc,
	src, dst string,
	file *os.File,
	writer io.Writer,
	response *http.Response,
	offset, total int64,
	progress *progressbar.ProgressBar,
) (err error) {
	directory := downloadPartsPath(dst)
	if err := os.Mkdir(directory, 0700); err != nil {
		return fmt.Errorf("failed to create download staging directory: %w", err)
	}

	defer func() {
		if cleanupErr := os.RemoveAll(directory); cleanupErr != nil {
			err = joinCleanupError(err, "failed to remove download staging directory", cleanupErr)
		}
	}()

	chunkSize := (total - offset) / parallelDownloadCount
	validator := downloadValidator(response)

	var (
		workers  sync.WaitGroup
		failures [parallelDownloadCount - 1]error
	)

	for index := range parallelDownloadCount - 1 {
		workers.Add(1)

		go func() {
			defer workers.Done()

			start := offset + chunkSize*int64(index+1)
			end := start + chunkSize - 1

			if index == parallelDownloadCount-2 {
				end = total - 1
			}

			path := filepath.Join(directory, fmt.Sprintf("%d", index))
			failures[index] = t.downloadFileRange(ctx, src, path, start, end, total, validator, progress)

			if failures[index] != nil {
				cancel()
			}
		}()
	}

	_, copyErr := io.CopyN(writer, response.Body, chunkSize)
	// The initial request covers the entire remainder. Stop reading it once its
	// first quarter is written; the other requests supply the remaining ranges.
	_ = response.Body.Close()

	if copyErr != nil {
		cancel()
	}

	workers.Wait()

	for _, failure := range failures {
		if failure != nil {
			return failure
		}
	}

	if copyErr != nil {
		return fmt.Errorf("failed to download first range: %w", copyErr)
	}

	for index := range parallelDownloadCount - 1 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("download canceled: %w", err)
		}

		path := filepath.Join(directory, fmt.Sprintf("%d", index))
		if err := appendDownloadPart(file, path); err != nil {
			return err
		}
	}

	return nil
}

//nolint:nonamedreturns // Deferred close errors must propagate to callers.
func (t *appstore) downloadFileRange(ctx context.Context, src, dst string, start, end, total int64, validator string, progress *progressbar.ProgressBar) (err error) {
	req, err := t.httpClient.NewRequest(http.MethodGet, src, nil)
	if err != nil {
		return fmt.Errorf("failed to create range request: %w", err)
	}

	req = req.WithContext(ctx)
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	if validator != "" {
		req.Header.Set("If-Range", validator)
	}

	response, err := t.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("range request failed: %w", err)
	}
	defer response.Body.Close()

	_, length, size, _, err := downloadResponseRange(response, start)
	if err != nil {
		return err
	}

	if response.StatusCode != http.StatusPartialContent || length != end-start+1 || size != total {
		return fmt.Errorf("unexpected download range %q", response.Header.Get("Content-Range"))
	}

	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return fmt.Errorf("unexpected download content encoding %q", encoding)
	}

	if etag := response.Header.Get("ETag"); validator != "" && etag != "" && etag != validator {
		return fmt.Errorf("download validator changed from %q to %q", validator, etag)
	}

	file, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to create download part: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = joinCleanupError(err, "failed to close download part", closeErr)
		}
	}()

	var writer io.Writer = file
	if progress != nil {
		writer = io.MultiWriter(file, progress)
	}

	written, err := io.Copy(writer, io.LimitReader(response.Body, length+1))
	if err != nil {
		return fmt.Errorf("failed to write download part: %w", err)
	}

	if written != length {
		return fmt.Errorf("download part is incomplete: %w", io.ErrUnexpectedEOF)
	}

	return nil
}

func appendDownloadPart(destination io.Writer, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open download part: %w", err)
	}

	_, copyErr := io.Copy(destination, file)
	closeErr := file.Close()

	if copyErr != nil {
		return fmt.Errorf("failed to append download part: %w", copyErr)
	}

	if closeErr != nil {
		return fmt.Errorf("failed to close download part: %w", closeErr)
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to remove download part: %w", err)
	}

	return nil
}
