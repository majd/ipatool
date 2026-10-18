// Package store provides the portable, framework-backed App Store request engine.
package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/store/emulator"
	"github.com/majd/ipatool/v2/internal/store/profile"
	apphttp "github.com/majd/ipatool/v2/pkg/http"
)

type Backend struct {
	jar           http.CookieJar
	macAddress    func() (string, error)
	mu            sync.Mutex
	image         []byte
	bag           map[string]any
	transport     *http.Transport
	authTransport *http.Transport
}

type executor struct {
	backend   *Backend
	operation string
}

func New(jar http.CookieJar, macAddress func() (string, error)) *Backend {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.IdleConnTimeout = 90 * time.Second
	authTransport := transport.Clone()
	authTransport.DisableKeepAlives = true

	return &Backend{jar: jar, macAddress: macAddress, transport: transport, authTransport: authTransport}
}

// Client shares session cookies and bag data while isolating each guest VM.
func (b *Backend) Client(operation string) apphttp.Executor {
	return &executor{backend: b, operation: operation}
}

func (e *executor) Send(request apphttp.Request, body []byte) (*http.Response, error) {
	ctx := request.Context
	if ctx == nil {
		ctx = context.Background()
	}

	input := emulator.Request{URL: request.URL, Method: request.Method, Operation: e.operation, Format: string(request.ResponseFormat), Timeout: request.Timeout, Body: body, Headers: make(http.Header), NoRedirects: request.NoRedirects, Raw: request.ResponseFormat == apphttp.ResponseFormatRaw}
	for key, value := range request.Headers {
		input.Headers.Set(key, value)
	}

	if payload, ok := request.Payload.(*apphttp.XMLPayload); ok {
		input.Parameters = payload.Content
		if input.Parameters == nil {
			input.Parameters = map[string]any{}
		}
	}

	if request.ActionSigner != nil {
		input.Sign = request.ActionSigner.Sign
	}

	return e.backend.execute(ctx, input)
}

func (e *executor) Do(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, errors.New("store request is nil")
	}

	var body []byte

	var err error
	if request.Body != nil {
		body, err = io.ReadAll(io.LimitReader(request.Body, (1<<20)+1))
		_ = request.Body.Close()

		if err != nil {
			return nil, fmt.Errorf("read Store request: %w", err)
		}

		if len(body) > 1<<20 {
			return nil, errors.New("store request body exceeds limit")
		}
	}

	return e.backend.execute(request.Context(), emulator.Request{URL: request.URL.String(), Method: request.Method, Headers: request.Header.Clone(), Body: body, Operation: e.operation, Stream: e.operation == "transfer"})
}

func (b *Backend) hardware(parameters map[string]any) ([]byte, error) {
	if value, ok := parameters["guid"].(string); ok {
		data, err := hex.DecodeString(value)
		if err != nil || len(data) != 6 {
			return nil, errors.New("invalid Store hardware identity")
		}

		return data, nil
	}

	if b.macAddress == nil {
		return nil, errors.New("store machine identity is not configured")
	}

	value, err := b.macAddress()
	if err != nil {
		return nil, fmt.Errorf("get Store machine identity: %w", err)
	}

	data, err := net.ParseMAC(value)
	if err != nil || len(data) != 6 {
		return nil, errors.New("invalid Store machine address")
	}

	return data, nil
}

func (b *Backend) prepare(ctx context.Context) ([]byte, profile.Data, error) {
	prepared, err := profile.Load(ctx)
	if err != nil {
		return nil, profile.Data{}, fmt.Errorf("load Store runtime: %w", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.image == nil {
		bundle, err := assets.Load(ctx)
		if err != nil {
			return nil, profile.Data{}, fmt.Errorf("load Store runtime: %w", err)
		}

		b.image = bundle.AppleMediaServices
	}

	return b.image, prepared, nil
}

func (b *Backend) execute(ctx context.Context, input emulator.Request) (*http.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("store request: %w", err)
	}

	hardware, err := b.hardware(input.Parameters)
	if err != nil {
		return nil, fmt.Errorf("store request: %w", err)
	}

	input.HardwareID = hardware
	if input.Headers == nil {
		input.Headers = make(http.Header)
	}

	if input.Headers.Get("User-Agent") == "" {
		input.Headers.Set("User-Agent", apphttp.DefaultUserAgent)

		if input.Operation == "purchase" {
			input.Headers.Set("User-Agent", "Configurator/2.18 (Macintosh; OS X 15.3.2; 24D81) AppleWebKit/0620.2.4.11.6")
		}
	}

	b.mu.Lock()
	bag := b.bag
	b.mu.Unlock()

	if input.Operation == "purchase" && bag == nil {
		response, err := b.execute(ctx, emulator.Request{URL: "https://init.itunes.apple.com/bag.xml?guid=" + strings.ToUpper(hex.EncodeToString(hardware)), Method: http.MethodGet, Operation: "bag"})
		if err != nil {
			return nil, fmt.Errorf("load purchase bag: %w", err)
		}

		_ = response.Body.Close()

		b.mu.Lock()
		bag = b.bag
		b.mu.Unlock()
	}

	input.Bag = bag

	image, prepared, err := b.prepare(ctx)
	if err != nil {
		return nil, fmt.Errorf("prepare Store framework: %w", err)
	}

	guest, err := emulator.Open(ctx, image, prepared)
	if err != nil {
		return nil, fmt.Errorf("open Store framework: %w", err)
	}

	defer guest.Close()

	input.Transport = b.requestTransport(input)

	response, err := guest.Execute(input)
	if err != nil {
		return nil, fmt.Errorf("store request: %w", err)
	}

	if input.Stream {
		return response, nil
	}

	if strings.EqualFold(response.Header.Get("Content-Encoding"), "gzip") {
		reader, e := gzip.NewReader(response.Body)
		if e != nil {
			_ = response.Body.Close()

			return nil, errors.New("invalid compressed Store response")
		}

		data, e := io.ReadAll(io.LimitReader(reader, (32<<20)+1))
		_ = reader.Close()
		_ = response.Body.Close()

		if e != nil {
			return nil, fmt.Errorf("read compressed Store response: %w", e)
		}

		if len(data) > 32<<20 {
			return nil, errors.New("store response exceeds size limit")
		}

		response.Body = io.NopCloser(bytes.NewReader(data))
		response.Header.Del("Content-Encoding")
		response.Header.Del("Content-Length")
		response.ContentLength = int64(len(data))
	}

	if strings.Contains(input.URL, "init.itunes.apple.com/bag.xml") && response.StatusCode == http.StatusOK {
		data, e := io.ReadAll(response.Body)
		_ = response.Body.Close()

		if e != nil {
			return nil, fmt.Errorf("read Store response: %w", e)
		}

		response.Body = io.NopCloser(bytes.NewReader(data))

		var values map[string]any
		if e = apphttp.DecodePropertyList(data, &values); e == nil {
			b.mu.Lock()
			b.bag = values
			b.mu.Unlock()
		}
	}

	return response, nil
}

// CloseIdleConnections releases pooled sockets without interrupting active streams.
func (b *Backend) CloseIdleConnections() {
	b.transport.CloseIdleConnections()
	b.authTransport.CloseIdleConnections()
}

// Each request owns its timeout; compatible requests share only the connection pool.
// Login keeps its existing fresh-connection policy, including framework redirects.
func (b *Backend) requestTransport(input emulator.Request) func(*http.Request) (*http.Response, error) {
	timeout := input.Timeout
	if timeout == 0 && input.Operation == "api" {
		timeout = 30 * time.Second
	}

	transport := b.transport

	if input.Operation == "authenticate" {
		transport = b.authTransport
	}

	client := &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	return func(request *http.Request) (*http.Response, error) {
		if b.jar != nil && request.Header.Get("Cookie") == "" {
			for _, cookie := range b.jar.Cookies(request.URL) {
				request.AddCookie(cookie)
			}
		}

		response, err := client.Do(request)
		if err == nil && b.jar != nil {
			b.jar.SetCookies(request.URL, response.Cookies())
		}

		if err != nil {
			return nil, fmt.Errorf("store HTTP transport: %w", err)
		}

		return response, nil
	}
}
