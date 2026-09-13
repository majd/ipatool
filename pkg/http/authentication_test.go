package http

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type sessionCookieJar struct{ *cookiejar.Jar }

func (sessionCookieJar) Save() error { return nil }

var _ = Describe("Authentication transport", func() {
	DescribeTable("preserves cookies and signed POSTs across fresh connections",
		func(http2 bool) {
			jar, err := cookiejar.New(nil)
			Expect(err).NotTo(HaveOccurred())
			signer := &recordingActionSigner{signature: []byte("signature")}
			addresses := make(chan string, 4)
			calls := 0
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer GinkgoRecover()
				calls++
				addresses <- r.RemoteAddr
				Expect(r.ProtoMajor).To(Equal(map[bool]int{false: 1, true: 2}[http2]))
				Expect(r.Method).To(Equal(http.MethodPost))
				data, err := io.ReadAll(r.Body)
				Expect(err).NotTo(HaveOccurred())
				Expect(data).To(Equal(signer.data))
				Expect(r.Header.Get(HeaderAppleActionSignature)).To(Equal(base64.StdEncoding.EncodeToString(signer.signature)))
				if calls > 1 {
					cookie, err := r.Cookie("session")
					Expect(err).NotTo(HaveOccurred())
					Expect(cookie.Value).To(Equal("retained"))
				}
				switch calls {
				case 1:
					http.SetCookie(w, &http.Cookie{Name: "session", Value: "retained", Path: "/"})
					w.WriteHeader(http.StatusNoContent)
				case 2:
					w.Header().Set("Location", "https://p7-buy.itunes.apple.com"+appStoreAuthPath)
					w.WriteHeader(http.StatusFound)
				default:
					_, err = w.Write([]byte("<dict><key>foo</key><string>ok</string></dict>"))
					Expect(err).NotTo(HaveOccurred())
				}
			}))
			srv.EnableHTTP2 = http2
			srv.StartTLS()
			DeferCleanup(srv.Close)

			original := http.DefaultTransport.(*http.Transport).DisableKeepAlives
			sut := NewClient[struct {
				Foo string `plist:"foo"`
			}](Args{CookieJar: sessionCookieJar{jar}, Authentication: true}).(*client[struct {
				Foo string `plist:"foo"`
			}])
			transport := sut.internalClient.Transport.(*AddHeaderTransport).T.(*http.Transport)
			// Trust only the test server's certificate; retain the production transport settings.
			transport.TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			DeferCleanup(transport.CloseIdleConnections)
			request := Request{URL: srv.URL + appStoreAuthPath, Method: MethodPOST, ResponseFormat: ResponseFormatXML, ActionSigner: signer, Payload: &XMLPayload{Content: map[string]interface{}{"password": "password", "attempt": "1"}}}
			_, err = sut.Send(request)
			Expect(err).To(HaveOccurred())
			redirect, err := sut.Send(request)
			Expect(err).NotTo(HaveOccurred())
			Expect(redirect.StatusCode).To(Equal(http.StatusFound))
			Expect(redirect.Headers).To(HaveKey("Location"))
			// The appstore layer validates the pod URL and explicitly repeats the POST.
			// Route that POST to this server to inspect it without contacting Apple.
			_, err = sut.Send(request)
			Expect(err).NotTo(HaveOccurred())
			request.Payload = &XMLPayload{Content: map[string]interface{}{"password": "password123456", "attempt": "1"}}
			_, err = sut.Send(request)
			Expect(err).NotTo(HaveOccurred())
			Expect(signer.calls).To(Equal(4))
			seen := map[string]bool{}
			for range 4 {
				seen[<-addresses] = true
			}
			Expect(seen).To(HaveLen(4))
			Expect(http.DefaultTransport.(*http.Transport).DisableKeepAlives).To(Equal(original))
		}, Entry("HTTP/1.1", false), Entry("HTTP/2", true),
	)

	It("preserves a populated Apple error even with HTTP 429", func() {
		recorder := httptest.NewRecorder()
		recorder.Header().Set("Retry-After", "5")
		recorder.WriteHeader(http.StatusTooManyRequests)
		_, err := recorder.WriteString("<dict><key>failureType</key><string>badCredentials</string></dict>")
		Expect(err).NotTo(HaveOccurred())
		sut := &client[map[string]string]{authentication: true}
		result, err := sut.handleXMLResponse(recorder.Result())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Data).To(HaveKeyWithValue("failureType", "badCredentials"))
		Expect(result.StatusCode).To(Equal(http.StatusTooManyRequests))
	})

	It("retains pooling for ordinary clients", func() {
		addresses := make(chan string, 2)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addresses <- r.RemoteAddr
			w.WriteHeader(http.StatusNoContent)
		}))
		DeferCleanup(srv.Close)
		sut := NewClient[struct{}](Args{})
		for range 2 {
			request, err := http.NewRequest(http.MethodGet, srv.URL, nil)
			Expect(err).NotTo(HaveOccurred())
			response, err := sut.Do(request)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.Body.Close()).To(Succeed())
		}
		Expect(<-addresses).To(Equal(<-addresses))
	})

	DescribeTable("reports malformed responses without retaining credentials",
		func(status int, body, reason string) {
			recorder := httptest.NewRecorder()
			recorder.Header().Set("Content-Type", "text/html")
			recorder.Header().Set("X-Apple-Jingle-Correlation-Key", "correlation-123")
			recorder.Header().Set("Set-Cookie", "secret-cookie")
			recorder.Header().Set("Retry-After", "5")
			recorder.WriteHeader(status)
			_, err := recorder.WriteString(body)
			Expect(err).NotTo(HaveOccurred())
			sut := &client[struct{}]{authentication: true}
			_, err = sut.handleXMLResponse(recorder.Result())
			Expect(err).To(HaveOccurred())
			responseErr, ok := err.(*UnexpectedResponseError)
			Expect(ok).To(BeTrue())
			Expect(responseErr.StatusCode).To(Equal(status))
			Expect(responseErr.BodyLength).To(Equal(len(body)))
			Expect(responseErr.ContentType).To(Equal("text/html"))
			Expect(responseErr.CorrelationID).To(Equal("correlation-123"))
			Expect(responseErr.RetryAfter).To(Equal("5"))
			Expect(responseErr.Reason).To(Equal(reason))
			Expect(responseErr.Snippet).To(BeEmpty())
			Expect(err.Error()).NotTo(ContainSubstring("secret"))
		},
		Entry("empty forbidden response", 403, "", "empty or non-plist authentication response"),
		Entry("HTML failure", 500, "<html>secret-password</html>", "empty or non-plist authentication response"),
		Entry("redirect without Location", 302, "secret-token", "authentication redirect is missing Location"),
		Entry("rate limit", 429, "secret-token", "rate limited by Apple"),
		Entry("malformed plist", 200, "<plist><dict>secret-token", "malformed authentication plist"),
	)
})
