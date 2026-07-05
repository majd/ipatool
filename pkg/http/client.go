package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"howett.net/plist"
)

const (
	appStoreAuthURL = "https://buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/authenticate"
)

var (
	documentXMLPattern = regexp.MustCompile(`(?is)<Document\b[^>]*>(.*)</Document>`)
	plistXMLPattern    = regexp.MustCompile(`(?is)<plist\b[^>]*>.*?</plist>`)
	dictXMLPattern     = regexp.MustCompile(`(?is)<dict\b[^>]*>.*</dict>`)
	htmlTagPattern     = regexp.MustCompile(`(?is)<[^>]*>`)
)

//go:generate go run go.uber.org/mock/mockgen -source=client.go -destination=client_mock.go -package=http
type Client[R interface{}] interface {
	Send(request Request) (Result[R], error)
	Do(req *http.Request) (*http.Response, error)
	NewRequest(method, url string, body io.Reader) (*http.Request, error)
}

type client[R interface{}] struct {
	internalClient http.Client
	cookieJar      CookieJar
}

type Args struct {
	CookieJar CookieJar
}

type AddHeaderTransport struct {
	T http.RoundTripper
}

func (t *AddHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", DefaultUserAgent)
	}

	res, err := t.T.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make round trip: %w", err)
	}

	return res, nil
}

func NewClient[R interface{}](args Args) Client[R] {
	return &client[R]{
		internalClient: http.Client{
			Timeout: 0,
			Jar:     args.CookieJar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if req.Referer() == appStoreAuthURL {
					return http.ErrUseLastResponse
				}

				return nil
			},
			Transport: &AddHeaderTransport{http.DefaultTransport},
		},
		cookieJar: args.CookieJar,
	}
}

func (c *client[R]) Send(req Request) (Result[R], error) {
	var (
		data []byte
		err  error
	)

	if req.Payload != nil {
		data, err = req.Payload.data()
		if err != nil {
			return Result[R]{}, fmt.Errorf("failed to get payload data: %w", err)
		}
	}

	request, err := http.NewRequest(req.Method, req.URL, bytes.NewReader(data))
	if err != nil {
		return Result[R]{}, fmt.Errorf("failed to create request: %w", err)
	}

	for key, val := range req.Headers {
		request.Header.Set(key, val)
	}

	res, err := c.internalClient.Do(request)
	if err != nil {
		return Result[R]{}, fmt.Errorf("request failed: %w", err)
	}
	defer res.Body.Close()

	err = c.cookieJar.Save()
	if err != nil {
		return Result[R]{}, fmt.Errorf("failed to save cookies: %w", err)
	}

	if req.ResponseFormat == ResponseFormatJSON {
		return c.handleJSONResponse(res)
	}

	if req.ResponseFormat == ResponseFormatXML {
		return c.handleXMLResponse(res)
	}

	return Result[R]{}, fmt.Errorf("content type is not supported (%s)", req.ResponseFormat)
}

func (c *client[R]) Do(req *http.Request) (*http.Response, error) {
	res, err := c.internalClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("received error: %w", err)
	}

	return res, nil
}

func (*client[R]) NewRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	return req, nil
}

func (c *client[R]) handleJSONResponse(res *http.Response) (Result[R], error) {
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return Result[R]{}, fmt.Errorf("failed to read response body: %w", err)
	}

	var data R

	err = json.Unmarshal(body, &data)
	if err != nil {
		return Result[R]{}, fmt.Errorf("failed to unmarshal json: %w", err)
	}

	return Result[R]{
		StatusCode: res.StatusCode,
		Data:       data,
	}, nil
}

func (c *client[R]) handleXMLResponse(res *http.Response) (Result[R], error) {
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return Result[R]{}, fmt.Errorf("failed to read response body: %w", err)
	}

	if res.StatusCode == http.StatusTooManyRequests {
		return Result[R]{}, fmt.Errorf("rate limited by Apple (HTTP %d): %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	var data R

	normalizedBody := normalizeXMLPlistBody(body)

	if !looksLikePropertyList(normalizedBody) {
		snippet := bodySnippet(body)
		if snippet == "" {
			return Result[R]{}, fmt.Errorf("unexpected response from Apple (HTTP %d): empty or non-plist body", res.StatusCode)
		}

		return Result[R]{}, fmt.Errorf("unexpected response from Apple (HTTP %d): %s", res.StatusCode, snippet)
	}

	_, err = plist.Unmarshal(normalizedBody, &data)
	if err != nil {
		return Result[R]{}, fmt.Errorf("failed to unmarshal xml: %w", err)
	}

	headers := map[string]string{}
	for key, val := range res.Header {
		headers[key] = strings.Join(val, "; ")
	}

	return Result[R]{
		StatusCode: res.StatusCode,
		Headers:    headers,
		Data:       data,
	}, nil
}

func normalizeXMLPlistBody(body []byte) []byte {
	normalized := bytes.TrimSpace(body)
	if len(normalized) == 0 {
		return normalized
	}

	if documentBody := extractDocumentInnerBody(normalized); len(documentBody) > 0 {
		normalized = documentBody
	}

	if embeddedPlist := extractEmbeddedPlist(normalized); len(embeddedPlist) > 0 {
		normalized = embeddedPlist
	}

	if dictBody := extractEmbeddedDict(normalized); len(dictBody) > 0 {
		return dictBody
	}

	if bytes.Contains(normalized, []byte("<key>")) {
		return []byte("<dict>" + string(normalized) + "</dict>")
	}

	return normalized
}

// looksLikePropertyList reports whether body appears to be a (binary or XML)
// property list. Apple occasionally answers with an HTML error page or a plain
// text message; those must not be handed to plist.Unmarshal, which would
// misinterpret a leading "<h..." as an OpenStep hex-data block and fail with an
// opaque "unexpected hex digit" error instead of surfacing Apple's actual response.
func looksLikePropertyList(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return false
	}

	if bytes.HasPrefix(trimmed, []byte("bplist")) {
		return true
	}

	lower := bytes.ToLower(trimmed)
	for _, marker := range [][]byte{
		[]byte("<?xml"),
		[]byte("<plist"),
		[]byte("<dict"),
		[]byte("<key"),
	} {
		if bytes.Contains(lower, marker) {
			return true
		}
	}

	return false
}

// bodySnippet returns a compact, single-line excerpt of a non-plist response
// body suitable for embedding in an error message. HTML markup is stripped so
// the underlying message (if any) is readable.
func bodySnippet(body []byte) string {
	text := htmlTagPattern.ReplaceAll(body, []byte(" "))
	snippet := strings.Join(strings.Fields(string(text)), " ")

	const maxLen = 200
	if len(snippet) > maxLen {
		snippet = snippet[:maxLen] + "…"
	}

	return snippet
}

func extractEmbeddedPlist(body []byte) []byte {
	plistMatch := plistXMLPattern.Find(body)
	if len(plistMatch) == 0 {
		return nil
	}

	return bytes.TrimSpace(plistMatch)
}

func extractEmbeddedDict(body []byte) []byte {
	dictMatch := dictXMLPattern.Find(body)
	if len(dictMatch) == 0 {
		return nil
	}

	return bytes.TrimSpace(dictMatch)
}

func extractDocumentInnerBody(body []byte) []byte {
	documentMatch := documentXMLPattern.FindSubmatch(body)
	if len(documentMatch) < 2 {
		return nil
	}

	documentBody := bytes.TrimSpace(documentMatch[1])
	if len(documentBody) == 0 {
		return nil
	}

	return documentBody
}
