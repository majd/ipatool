//nolint:wsl // Trust setup follows certificate validation order.
package storeauth

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	appleRootURL  = "https://www.apple.com/appleca/AppleIncRootCertificate.cer"
	rootSizeLimit = 64 << 10
)

// NewHTTPClient returns the isolated client used for Apple authentication.
func NewHTTPClient(jar http.CookieJar) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}

	return &http.Client{
		Transport: transport,
		Jar:       jar,
		Timeout:   30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func isUnknownAuthority(err error) bool {
	var unknown x509.UnknownAuthorityError

	return errors.As(err, &unknown)
}

func installAppleRoot(ctx context.Context, client *http.Client) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, appleRootURL, nil)
	if err != nil {
		return fmt.Errorf("create Apple root request: %w", err)
	}

	downloader := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}},
		Timeout:   15 * time.Second,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if request.URL.Scheme != "https" || request.URL.Hostname() != "www.apple.com" {
				return errors.New("apple root redirect is not trusted")
			}

			return nil
		},
	}

	response, err := downloader.Do(request)
	if err != nil {
		return fmt.Errorf("download Apple root: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download Apple root: HTTP %d", response.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, rootSizeLimit+1))
	if err != nil {
		return fmt.Errorf("read Apple root: %w", err)
	}
	if len(data) > rootSizeLimit {
		return errors.New("apple root exceeds size limit")
	}

	certificate, err := x509.ParseCertificate(data)
	if err != nil {
		return fmt.Errorf("parse Apple root: %w", err)
	}
	if !validAppleRoot(certificate, time.Now()) {
		return errors.New("downloaded Apple root is invalid")
	}

	roots, _ := x509.SystemCertPool()
	if roots == nil {
		roots = x509.NewCertPool()
	}
	roots.AddCert(certificate)

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		return errors.New("apple authentication transport is unsupported")
	}
	updated := transport.Clone()
	updated.TLSClientConfig = transport.TLSClientConfig.Clone()
	updated.TLSClientConfig.RootCAs = roots
	updated.TLSClientConfig.MinVersion = tls.VersionTLS12
	transport.CloseIdleConnections()
	client.Transport = updated

	return nil
}

func validAppleRoot(certificate *x509.Certificate, now time.Time) bool {
	if certificate == nil || !certificate.IsCA || !certificate.BasicConstraintsValid ||
		certificate.KeyUsage&x509.KeyUsageCertSign == 0 || certificate.Subject.CommonName != "Apple Root CA" ||
		!bytes.Equal(certificate.RawSubject, certificate.RawIssuer) || now.Before(certificate.NotBefore) || now.After(certificate.NotAfter) {
		return false
	}

	for _, organization := range certificate.Subject.Organization {
		if organization == "Apple Inc." {
			return true
		}
	}

	return false
}
