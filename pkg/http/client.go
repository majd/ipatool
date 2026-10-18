package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"howett.net/plist"
)

const DefaultAuthenticationTimeout = 30 * time.Second

// IsAuthenticationRedirect reports which Store redirects can replay a signed POST.
// A 303 explicitly requests a GET and is not part of this authentication flow.
func IsAuthenticationRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

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
	executor       Executor
	timeout        time.Duration
	cookieJar      CookieJar
	authentication bool
}

type Args struct {
	Executor  Executor
	CookieJar CookieJar
	// Authentication enables allowlisted login diagnostics and its default timeout.
	// The executor owns connection isolation and redirect handling.
	Authentication bool
	// Timeout covers the request and response body. Zero uses the authentication
	// default for login clients and leaves other clients without an overall limit.
	Timeout time.Duration
}

// UnexpectedResponseError preserves the HTTP status when Apple returns an
// HTML or empty response where an XML plist was expected.
type UnexpectedResponseError struct {
	StatusCode    int
	Snippet       string
	Reason        string
	BodyLength    int
	ContentType   string
	CorrelationID string
	RetryAfter    string
}

func (e *UnexpectedResponseError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("unexpected response from Apple (HTTP %d): %s (body length=%d, content type=%q, correlation ID=%q)", e.StatusCode, e.Reason, e.BodyLength, e.ContentType, e.CorrelationID)
	}

	if e.Snippet == "" {
		return fmt.Sprintf("unexpected response from Apple (HTTP %d): empty or non-plist body", e.StatusCode)
	}

	return fmt.Sprintf("unexpected response from Apple (HTTP %d): %s", e.StatusCode, e.Snippet)
}

// NewClient creates a typed response adapter. Requests require an explicit
// executor; production Store clients always supply the framework backend.
func NewClient[R interface{}](args Args) Client[R] {
	timeout := args.Timeout
	if args.Authentication && timeout == 0 {
		timeout = DefaultAuthenticationTimeout
	}

	return &client[R]{executor: args.Executor, timeout: timeout, cookieJar: args.CookieJar, authentication: args.Authentication}
}

func (c *client[R]) Send(req Request) (Result[R], error) {
	var data []byte
	if payload, ok := req.Payload.(*RawPayload); ok {
		data = bytes.Clone(payload.Content)
	}

	if c.executor == nil {
		return Result[R]{}, errors.New("request executor is not configured")
	}

	req.Timeout = c.timeout
	res, err := c.executor.Send(req, data)

	if err != nil {
		return Result[R]{}, fmt.Errorf("request failed: %w", &TransportError{Err: err})
	}

	if res == nil || res.Body == nil {
		return Result[R]{}, errors.New("request returned no HTTP response")
	}

	defer res.Body.Close()

	if c.cookieJar != nil {
		err = c.cookieJar.Save()
	}

	if err != nil {
		return Result[R]{}, fmt.Errorf("failed to save cookies: %w", err)
	}

	if req.ResponseFormat == ResponseFormatJSON {
		return c.handleJSONResponse(res)
	}

	if req.ResponseFormat == ResponseFormatRaw {
		return c.handleRawResponse(res)
	}

	if req.ResponseFormat == ResponseFormatXML {
		return c.handleXMLResponse(res)
	}

	return Result[R]{}, fmt.Errorf("content type is not supported (%s)", req.ResponseFormat)
}

func (c *client[R]) handleRawResponse(res *http.Response) (Result[R], error) {
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return Result[R]{}, fmt.Errorf("failed to read response body: %w", &TransportError{Err: err, RetryAfter: res.Header.Get("Retry-After")})
	}

	data, ok := any(body).(R)
	if !ok {
		return Result[R]{}, errors.New("raw response format requires a []byte result type")
	}

	return Result[R]{
		StatusCode: res.StatusCode,
		Headers:    responseHeaders(res),
		Data:       data,
	}, nil
}

func (c *client[R]) Do(req *http.Request) (*http.Response, error) {
	if c.executor == nil {
		return nil, errors.New("request executor is not configured")
	}

	res, err := c.executor.Do(req)
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
		return Result[R]{}, fmt.Errorf("failed to read response body: %w", &TransportError{Err: err, RetryAfter: res.Header.Get("Retry-After")})
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
		return Result[R]{}, fmt.Errorf("failed to read response body: %w", &TransportError{Err: err, RetryAfter: res.Header.Get("Retry-After")})
	}

	if c.authentication && IsAuthenticationRedirect(res.StatusCode) && strings.TrimSpace(res.Header.Get("Location")) == "" {
		return Result[R]{}, authenticationResponseError(res, body, "authentication redirect is missing Location")
	}

	if !c.authentication && res.StatusCode == http.StatusTooManyRequests {
		return Result[R]{}, fmt.Errorf("rate limited by Apple (HTTP %d): %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	// The legacy authentication endpoint redirects to an assigned Store pod.
	// Preserve the redirect response and its Location header so callers can
	// repeat the original POST request at that pod.
	if res.StatusCode >= http.StatusMultipleChoices && res.StatusCode < http.StatusBadRequest {
		return Result[R]{
			StatusCode: res.StatusCode,
			Headers:    responseHeaders(res),
		}, nil
	}

	var data R

	normalizedBody := normalizeXMLPlistBody(body)

	if !looksLikePropertyList(normalizedBody) {
		if c.authentication {
			if res.StatusCode == http.StatusTooManyRequests {
				return Result[R]{}, authenticationResponseError(res, body, "rate limited by Apple")
			}

			return Result[R]{}, authenticationResponseError(res, body, "empty or non-plist authentication response")
		}

		snippet := bodySnippet(body)

		return Result[R]{}, &UnexpectedResponseError{
			StatusCode: res.StatusCode,
			Snippet:    snippet,
		}
	}

	_, err = plist.Unmarshal(normalizedBody, &data)
	if err != nil {
		if c.authentication {
			return Result[R]{}, authenticationResponseError(res, body, "malformed authentication plist")
		}

		return Result[R]{}, fmt.Errorf("failed to unmarshal xml: %w", err)
	}

	return Result[R]{
		StatusCode: res.StatusCode,
		Headers:    responseHeaders(res),
		Data:       data,
	}, nil
}

// DecodePropertyList accepts the legacy XML envelope used by Apple's URL bag.
// The framework backend uses the same normalization as the typed HTTP client.
func DecodePropertyList(data []byte, value any) error {
	if _, err := plist.Unmarshal(normalizeXMLPlistBody(data), value); err != nil {
		return fmt.Errorf("decode Apple property list: %w", err)
	}

	return nil
}

func responseHeaders(res *http.Response) map[string]string {
	headers := map[string]string{}
	for key, val := range res.Header {
		headers[key] = strings.Join(val, "; ")
	}

	return headers
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

// Do not retain bodies or arbitrary response headers: authentication responses
// can contain credentials, tokens and cookies, including on failure.
func authenticationResponseError(res *http.Response, body []byte, reason string) *UnexpectedResponseError {
	return &UnexpectedResponseError{
		StatusCode:    res.StatusCode,
		Reason:        reason,
		BodyLength:    len(body),
		ContentType:   bodySnippet([]byte(res.Header.Get("Content-Type"))),
		CorrelationID: bodySnippet([]byte(res.Header.Get("X-Apple-Jingle-Correlation-Key"))),
		RetryAfter:    res.Header.Get("Retry-After"),
	}
}
