package emulator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"
)

// Temporary opt-in diagnostics: the response body may contain account data.
func tracePurchaseResponse(request *http.Request, response *http.Response, body []byte) error {
	tracePath := os.Getenv("IPATOOL_PURCHASE_TRACE")
	if tracePath == "" || !strings.HasSuffix(request.URL.Path, "/buyProduct") {
		return nil
	}

	selectHeaders := func(headers http.Header, names ...string) http.Header {
		selected := http.Header{}

		for _, name := range names {
			if value := headers.Get(name); value != "" {
				selected.Set(name, value)
			}
		}

		return selected
	}

	record := struct {
		URL             string
		Method          string
		RequestHeaders  http.Header
		StatusCode      int
		ResponseHeaders http.Header
		Body            string `json:",omitempty"`
		BodyBase64      []byte `json:",omitempty"`
	}{
		URL: request.URL.String(), Method: request.Method,
		RequestHeaders:  selectHeaders(request.Header, "X-Apple-Store-Front", "User-Agent", "X-Apple-Client-Application", "Content-Type"),
		StatusCode:      response.StatusCode,
		ResponseHeaders: selectHeaders(response.Header, "X-Set-Apple-Store-Front", "X-Apple-Request-Store-Front", "Location", "Pod", "Content-Type", "Content-Encoding", "X-Apple-Orig-Url"),
	}

	if utf8.Valid(body) {
		record.Body = string(body)
	} else {
		record.BodyBase64 = body
	}

	file, err := os.OpenFile(tracePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("open purchase trace: %w", err)
	}

	defer file.Close()

	if err = file.Chmod(0600); err != nil {
		return fmt.Errorf("protect purchase trace: %w", err)
	}

	if err = json.NewEncoder(file).Encode(record); err != nil {
		return fmt.Errorf("write purchase trace: %w", err)
	}

	return nil
}
