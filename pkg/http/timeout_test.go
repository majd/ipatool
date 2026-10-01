package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type timeoutTestTransport func(*http.Request) (*http.Response, error)

func (t timeoutTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return t(request)
}

type timeoutTestJar struct {
	*cookiejar.Jar
	saveErr error
}

func (j timeoutTestJar) Save() error { return j.saveErr }

var _ = Describe("Request timeouts", func() {
	DescribeTable("applies deadlines to bounded clients without limiting downloads", func(args Args, want time.Duration) {
		jar, err := cookiejar.New(nil)
		Expect(err).NotTo(HaveOccurred())
		args.CookieJar = sessionCookieJar{jar}
		sut := NewClient[[]byte](args).(*client[[]byte])
		sut.internalClient.Transport = timeoutTestTransport(func(request *http.Request) (*http.Response, error) {
			deadline, ok := request.Context().Deadline()
			Expect(ok).To(Equal(want > 0))
			if ok {
				Expect(time.Until(deadline)).To(BeNumerically(">", want-time.Second))
				Expect(time.Until(deadline)).To(BeNumerically("<=", want))
			}

			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("body"))}, nil
		})
		result, err := sut.Send(Request{URL: "https://example.com", Method: MethodGET, ResponseFormat: ResponseFormatRaw})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Data).To(Equal([]byte("body")))
	},
		Entry("authentication default", Args{Authentication: true}, 30*time.Second),
		Entry("authentication override", Args{Authentication: true, Timeout: 5 * time.Second}, 5*time.Second),
		Entry("bounded bag client", Args{Timeout: DefaultAuthenticationTimeout}, 30*time.Second),
		Entry("ordinary download client", Args{}, time.Duration(0)),
	)

	DescribeTable("interrupts stalled authentication responses", func(http2, bodyStarted bool) {
		jar, err := cookiejar.New(nil)
		Expect(err).NotTo(HaveOccurred())
		releaseResponse := make(chan struct{})
		srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer GinkgoRecover()
			Expect(r.ProtoMajor).To(Equal(map[bool]int{false: 1, true: 2}[http2]))
			if bodyStarted {
				_, err := w.Write([]byte("<dict><key>passwordToken</key>"))
				Expect(err).NotTo(HaveOccurred())
				w.(http.Flusher).Flush()
			}
			// Keep the response incomplete until Send returns; finishing on
			// server-side cancellation can race with the client's body read.
			<-releaseResponse
		}))
		srv.EnableHTTP2 = http2
		srv.StartTLS()
		DeferCleanup(srv.Close)
		DeferCleanup(func() { close(releaseResponse) })
		sut := NewClient[struct{}](Args{Authentication: true, CookieJar: sessionCookieJar{jar}, Timeout: 250 * time.Millisecond}).(*client[struct{}])
		transport := sut.internalClient.Transport.(*AddHeaderTransport).T.(*http.Transport)
		transport.TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
		DeferCleanup(transport.CloseIdleConnections)
		_, err = sut.Send(Request{URL: srv.URL + appStoreAuthPath, Method: MethodPOST, ResponseFormat: ResponseFormatXML})
		Expect(err).To(HaveOccurred())
		var transportErr *TransportError
		Expect(errors.As(err, &transportErr)).To(BeTrue(), "expected a transport failure, got %v", err)
		// A url.Error need not report Timeout when a transport wrapper sits
		// between it and the deadline error. Assert the underlying cause.
		Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue(), "expected a deadline failure, got %v", err)
		if bodyStarted {
			Expect(err.Error()).To(HavePrefix("failed to read response body:"))
		} else {
			Expect(err.Error()).To(HavePrefix("request failed:"))
		}
	},
		Entry("waiting for HTTP/1.1 headers", false, false),
		Entry("reading an HTTP/1.1 body", false, true),
		Entry("waiting for HTTP/2 headers", true, false),
		Entry("reading an HTTP/2 body", true, true),
	)

	DescribeTable("keeps local timeouts distinct from transport failures", func(signing bool) {
		jar, err := cookiejar.New(nil)
		Expect(err).NotTo(HaveOccurred())
		args := Args{Authentication: true, CookieJar: timeoutTestJar{Jar: jar, saveErr: context.DeadlineExceeded}}
		sut := NewClient[struct{}](args).(*client[struct{}])
		sut.internalClient.Transport = timeoutTestTransport(func(request *http.Request) (*http.Response, error) {
			Expect(signing).To(BeFalse())

			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("<dict/>"))}, nil
		})
		request := Request{URL: "https://example.com", Method: MethodPOST, ResponseFormat: ResponseFormatXML}
		if signing {
			request.ActionSigner = &recordingActionSigner{err: context.DeadlineExceeded}
		}
		_, err = sut.Send(request)
		Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
		var transportErr *TransportError
		Expect(errors.As(err, &transportErr)).To(BeFalse())
	}, Entry("signing", true), Entry("saving cookies", false))
})
