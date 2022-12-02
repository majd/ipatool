package http

import (
	"bytes"
	"encoding/json"
	"github.com/pkg/errors"
	"howett.net/plist"
	"io"
	"net/http"
	"strings"
	"time"
)

//go:generate mockgen -source=client.go -destination=client_mock.go -package=http
type Client[R interface{}] interface {
	Send(request Request) (Result[R], error)
	Do(req *http.Request) (*http.Response, error)
	NewRequest(method, url string, body io.Reader) (*http.Request, error)
}

type client[R interface{}] struct {
	internalClient http.Client
	cookieJar      CookieJar
}

type ClientArgs struct {
	CookieJar CookieJar
}

func NewClient[R interface{}](args ClientArgs) Client[R] {
	return &client[R]{
		internalClient: http.Client{
			Timeout: time.Second * 15,
			Jar:     args.CookieJar,
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
			return Result[R]{}, errors.Wrap(err, "failed to read payload data")
		}
	}

	request, err := http.NewRequest(req.Method, req.URL, bytes.NewReader(data))
	if err != nil {
		return Result[R]{}, errors.Wrap(err, "failed to create HTTP request")
	}

	for key, val := range req.Headers {
		request.Header.Set(key, val)
	}

	res, err := c.internalClient.Do(request)
	if err != nil {
		return Result[R]{}, errors.Wrap(err, "request failed")
	}

	err = c.cookieJar.Save()
	if err != nil {
		return Result[R]{}, errors.Wrap(err, "failed to save cookies")
	}

	if req.ResponseFormat == ResponseFormatJSON {
		return c.handleJSONResponse(res)
	}
	if req.ResponseFormat == ResponseFormatXML {
		return c.handleXMLResponse(res)
	}

	return Result[R]{}, errors.Errorf("unsupported response body content type: %s", req.ResponseFormat)
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
		return Result[R]{}, errors.Wrap(err, "failed to read response body")
	}

	var data R
	err = json.Unmarshal(body, &data)
	if err != nil {
		return Result[R]{}, errors.Wrap(err, "failed to unmarshall JSON data")
	}

	return Result[R]{
		StatusCode: res.StatusCode,
		Data:       data,
	}, nil
}

func (c *client[R]) handleXMLResponse(res *http.Response) (Result[R], error) {
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return Result[R]{}, errors.Wrap(err, "failed to read response body")
	}

	var data R
	_, err = plist.Unmarshal(body, &data)
	if err != nil {
		return Result[R]{}, errors.Wrap(err, "failed to unmarshall XML data")
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
