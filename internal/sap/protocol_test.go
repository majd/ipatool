package sap

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"howett.net/plist"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestSetupProtocol(t *testing.T) {
	protocol := setupProtocol{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("User-Agent") == "" {
			t.Error("User-Agent is empty")
		}
		key := setupCertificateKey
		value := []byte("certificate")
		if request.Method == http.MethodPost {
			if request.Header.Get("Content-Type") != "application/x-plist" {
				t.Errorf("Content-Type = %q", request.Header.Get("Content-Type"))
			}
			requestBody, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := plistBytes(requestBody, setupBufferKey)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(decoded, []byte("request")) {
				t.Errorf("request data = %q", decoded)
			}
			key = setupBufferKey
			value = []byte("reply")
		}
		body, err := plist.Marshal(map[string]any{key: value}, plist.XMLFormat)
		if err != nil {
			t.Fatal(err)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}}

	certificate, err := protocol.certificate(context.Background(), "https://example.com/certificate")
	if err != nil {
		t.Fatal(err)
	}

	if string(certificate) != "certificate" {
		t.Fatalf("certificate = %q", certificate)
	}

	reply, err := protocol.exchange(context.Background(), "https://example.com/setup", []byte("request"))
	if err != nil {
		t.Fatal(err)
	}

	if string(reply) != "reply" {
		t.Fatalf("reply = %q", reply)
	}
}

func TestSetupProtocolRejectsOversizedResponse(t *testing.T) {
	protocol := setupProtocol{client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", int(maxSetupBody)+1))),
			Header:     make(http.Header),
		}, nil
	})}}
	if _, err := protocol.certificate(context.Background(), "https://example.com/certificate"); err == nil {
		t.Fatal("oversized response was accepted")
	}
}
