package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"howett.net/plist"
	"io"
	"net/http"
	"strings"
)

//go:generate go run github.com/golang/mock/mockgen -source=client.go -destination=client_mock.go -package=http
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

func (adt *AddHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", DefaultUserAgent)
	}
	return adt.T.RoundTrip(req)
}

func NewClient[R interface{}](args Args) Client[R] {
	return &client[R]{
		internalClient: http.Client{
			Timeout:   0,
			Jar:       args.CookieJar,
			Transport: &AddHeaderTransport{http.DefaultTransport},
		},
		cookieJar: args.CookieJar,
	}
}

func (c *client[R]) Send(req Request) (Result[R], error) {
	var data []byte
	var err error

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
	return c.internalClient.Do(req)
}

func (*client[R]) NewRequest(method, url string, body io.Reader) (*http.Request, error) {
	return http.NewRequest(method, url, body)
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

	var data R
	_, err = plist.Unmarshal(body, &data)
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
