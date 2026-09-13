package appstore

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// downloadResponseRange validates the response before any partial bytes change.
// It returns the write offset, remaining bytes, total size, and completion state.
func downloadResponseRange(res *http.Response, localSize int64) (int64, int64, int64, bool, error) {
	switch res.StatusCode {
	case http.StatusOK:
		return 0, res.ContentLength, res.ContentLength, false, nil
	case http.StatusRequestedRangeNotSatisfiable:
		value, found := strings.CutPrefix(res.Header.Get("Content-Range"), "bytes */")
		size, parseErr := strconv.ParseInt(value, 10, 64)

		if found && parseErr == nil && size >= 0 && size == localSize {
			return localSize, 0, size, true, nil
		}

		return 0, 0, 0, false, fmt.Errorf("download range rejected: local size %d does not match server range %q", localSize, res.Header.Get("Content-Range"))
	case http.StatusPartialContent:
		header := res.Header.Get("Content-Range")
		value, found := strings.CutPrefix(header, "bytes ")
		bounds, totalText, hasTotal := strings.Cut(value, "/")
		startText, endText, hasEnd := strings.Cut(bounds, "-")
		start, startErr := strconv.ParseInt(startText, 10, 64)
		end, endErr := strconv.ParseInt(endText, 10, 64)
		size, sizeErr := strconv.ParseInt(totalText, 10, 64)

		if !found || !hasTotal || !hasEnd || startErr != nil || endErr != nil || sizeErr != nil || start != localSize || start < 0 || end < start || size <= end {
			return 0, 0, 0, false, fmt.Errorf("invalid download content range %q for local size %d", header, localSize)
		}

		length := end - start + 1
		if res.ContentLength >= 0 && res.ContentLength != length {
			return 0, 0, 0, false, fmt.Errorf("download content length %d does not match range length %d", res.ContentLength, length)
		}

		return start, length, size, false, nil
	default:
		return 0, 0, 0, false, fmt.Errorf("unexpected download response status: %d", res.StatusCode)
	}
}
