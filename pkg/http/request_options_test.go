package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("HTTP request options", func() {
	var jar *MockCookieJar
	BeforeEach(func() {
		jar = NewMockCookieJar(gomock.NewController(GinkgoT()))
		jar.EXPECT().Cookies(gomock.Any()).Return(nil).AnyTimes()
		jar.EXPECT().Save().Return(nil).AnyTimes()
	})

	It("stops redirects for a token-bearing request without changing subsequent requests", func() {
		var visited atomic.Int32
		destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			visited.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		defer destination.Close()
		source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
		}))
		defer source.Close()
		client := NewClient[[]byte](Args{CookieJar: jar})
		request := Request{URL: source.URL, Method: MethodPOST, ResponseFormat: ResponseFormatRaw, NoRedirects: true, Headers: map[string]string{"X-Token": "test-token"}}
		result, err := client.Send(request)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.StatusCode).To(Equal(http.StatusTemporaryRedirect))
		Expect(visited.Load()).To(BeZero())

		result, err = client.Send(Request{URL: source.URL, Method: MethodGET, ResponseFormat: ResponseFormatRaw})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.StatusCode).To(Equal(http.StatusOK))
		Expect(visited.Load()).To(Equal(int32(1)))
	})

	It("honors a request deadline while reading the response body", func() {
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-release
		}))
		defer server.Close()
		defer close(release)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		client := NewClient[[]byte](Args{CookieJar: jar})
		_, err := client.Send(Request{Context: ctx, URL: server.URL, Method: MethodGET, ResponseFormat: ResponseFormatRaw})
		Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
	})

	It("does not send a canceled request", func() {
		var visited atomic.Bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { visited.Store(true) }))
		defer server.Close()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		client := NewClient[[]byte](Args{CookieJar: jar})
		_, err := client.Send(Request{Context: ctx, URL: server.URL, Method: MethodGET, ResponseFormat: ResponseFormatRaw})
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(visited.Load()).To(BeFalse())
	})
})
