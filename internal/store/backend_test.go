package store

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/majd/ipatool/v2/internal/store/emulator"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestStoreBackend(t *testing.T) { RegisterFailHandler(Fail); RunSpecs(t, "Store Backend") }

var _ = Describe("Store connection reuse", func() {
	var backend *Backend
	var server *httptest.Server
	BeforeEach(func() {
		jar, err := cookiejar.New(nil)
		Expect(err).NotTo(HaveOccurred())
		backend = New(jar, nil)
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Connection", r.RemoteAddr)
			w.Header().Set("X-Received-Cookie", r.Header.Get("Cookie"))
			switch r.URL.Path {
			case "/cookie":
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "fixture", Path: "/"})
			case "/redirect":
				w.Header().Set("Location", "/target")
				w.WriteHeader(http.StatusFound)
			case "/slow":
				<-r.Context().Done()

				return
			case "/stream":
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				<-r.Context().Done()

				return
			}
			_, _ = io.WriteString(w, "fixture response")
		}))
	})
	AfterEach(func() {
		backend.CloseIdleConnections()
		server.Close()
	})
	get := func(operation, path string) *http.Response {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+path, nil)
		Expect(err).NotTo(HaveOccurred())
		response, err := backend.requestTransport(emulator.Request{Operation: operation})(req)
		Expect(err).NotTo(HaveOccurred())
		_, err = io.Copy(io.Discard, response.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(response.Body.Close()).To(Succeed())

		return response
	}
	It("reuses a consumed connection across API and download clients", func() {
		first := get("request", "/cookie")
		second := get("transfer", "/")
		Expect(second.Header.Get("X-Connection")).To(Equal(first.Header.Get("X-Connection")))
		Expect(second.Header.Get("X-Received-Cookie")).To(Equal("session=fixture"))
		third := get("bag", "/")
		Expect(third.Header.Get("X-Connection")).To(Equal(first.Header.Get("X-Connection")))
	})
	It("keeps authentication attempts separate without closing the API pool", func() {
		api := get("request", "/")
		first := get("authenticate", "/")
		second := get("authenticate", "/")
		Expect(first.Header.Get("X-Connection")).NotTo(Equal(api.Header.Get("X-Connection")))
		Expect(second.Header.Get("X-Connection")).NotTo(Equal(first.Header.Get("X-Connection")))
		Expect(get("request", "/").Header.Get("X-Connection")).To(Equal(api.Header.Get("X-Connection")))
	})
	It("returns redirects to the framework", func() {
		Expect(get("request", "/redirect").StatusCode).To(Equal(http.StatusFound))
	})
	It("preserves explicit cookie headers", func() {
		get("request", "/cookie")
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Cookie", "explicit=value")
		response, err := backend.requestTransport(emulator.Request{})(req)
		Expect(err).NotTo(HaveOccurred())
		defer response.Body.Close()
		Expect(response.Header.Get("X-Received-Cookie")).To(Equal("explicit=value"))
	})
	It("applies timeouts per request without poisoning subsequent requests", func() {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/slow", nil)
		Expect(err).NotTo(HaveOccurred())
		_, err = backend.requestTransport(emulator.Request{Timeout: 20 * time.Millisecond})(req)
		Expect(err).To(MatchError(ContainSubstring("deadline exceeded")))
		Expect(get("request", "/").StatusCode).To(Equal(http.StatusOK))
	})
	It("cancels an active stream without closing an unrelated pooled connection", func() {
		api := get("request", "/")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/stream", nil)
		Expect(err).NotTo(HaveOccurred())
		response, err := backend.requestTransport(emulator.Request{Operation: "transfer"})(req)
		Expect(err).NotTo(HaveOccurred())
		defer response.Body.Close()
		other := get("request", "/")
		Expect(other.Header.Get("X-Connection")).NotTo(Equal(api.Header.Get("X-Connection")))
		cancel()
		_, err = io.ReadAll(response.Body)
		Expect(err).To(MatchError(context.Canceled))
		Expect(response.Body.Close()).To(Succeed())
		Expect(get("request", "/").Header.Get("X-Connection")).To(Equal(other.Header.Get("X-Connection")))
	})
})
