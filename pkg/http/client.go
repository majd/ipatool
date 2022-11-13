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

type Client[R interface{}] interface {
	Send(r Request) (Result[R], error)
}

type client[R interface{}] struct {
	internalClient http.Client
	cookieJar      CookieJar
}

type Args struct {
	CookieJar CookieJar
}

func NewClient[R interface{}](args *Args) Client[R] {
	return &client[R]{
		internalClient: http.Client{
			Timeout: time.Second * 15,
			Jar:     args.CookieJar,
		},
		cookieJar: args.CookieJar,
	}
}

func (c *client[R]) Send(r Request) (Result[R], error) {
	var data []byte
	var err error

	if r.Payload != nil {
		data, err = r.Payload.data()
		if err != nil {
			return Result[R]{}, errors.Wrap(err, "failed to read payload data")
		}
	}

	request, err := http.NewRequest(r.Method, r.URL, bytes.NewReader(data))
	if err != nil {
		return Result[R]{}, errors.Wrap(err, "failed to create HTTP request")
	}

	for key, val := range r.Headers {
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

	ct := res.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		return c.handleJSONResponse(res)
	}
	if strings.Contains(ct, "application/xml") || strings.Contains(ct, "text/xml") {
		return c.handleXMLResponse(res)
	}

	return Result[R]{}, errors.Errorf("unsupported response body content type: %s", ct)
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
