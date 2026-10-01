package appstore

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/majd/ipatool/v2/pkg/keychain"
	"howett.net/plist"

	apphttp "github.com/majd/ipatool/v2/pkg/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("Authentication recovery", func() {
	DescribeTable("bounds transport retries and preserves the underlying error", func(cause error, transportFailure bool, calls int) {
		ctrl := gomock.NewController(GinkgoT())
		client := apphttp.NewMockClient[loginResult](ctrl)
		failure := cause
		if transportFailure {
			failure = &apphttp.TransportError{Err: cause}
		}
		failure = fmt.Errorf("request failed: %w", failure)
		client.EXPECT().Send(gomock.Any()).Return(apphttp.Result[loginResult]{}, failure).Times(calls)
		var waits []time.Duration
		sut := &appstore{loginClient: client, authRetrySleep: func(delay time.Duration) { waits = append(waits, delay) }}
		_, err := sut.sendAuthenticationRequest(apphttp.Request{})
		Expect(errors.Is(err, cause)).To(BeTrue())
		if calls == 1 {
			Expect(waits).To(BeEmpty())
		} else {
			Expect(waits).To(Equal([]time.Duration{10 * time.Second, 20 * time.Second}))
			Expect(err.Error()).To(ContainSubstring("after 3 attempts (transport error, transport error, transport error)"))
		}
	},
		Entry("request deadline", context.DeadlineExceeded, true, 3),
		Entry("request deadline wrapped by the header transport", &url.Error{Op: "Post", URL: testAuthEndpoint, Err: fmt.Errorf("failed to make round trip: %w", context.DeadlineExceeded)}, true, 3),
		Entry("wrapped DNS timeout", &url.Error{Op: "Post", URL: testAuthEndpoint, Err: &net.DNSError{IsTimeout: true}}, true, 3),
		Entry("DNS timeout wrapped by the header transport", &url.Error{Op: "Post", URL: testAuthEndpoint, Err: fmt.Errorf("failed to make round trip: %w", &net.DNSError{IsTimeout: true})}, true, 3),
		Entry("connection closed", io.EOF, true, 3),
		Entry("truncated response body", io.ErrUnexpectedEOF, true, 3),
		Entry("connection reset", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, true, 3),
		Entry("broken pipe", &net.OpError{Op: "write", Net: "tcp", Err: syscall.EPIPE}, true, 3),
		Entry("canceled request", context.Canceled, true, 1),
		Entry("canceled request wrapped by the header transport", &url.Error{Op: "Post", URL: testAuthEndpoint, Err: fmt.Errorf("failed to make round trip: %w", context.Canceled)}, true, 1),
		Entry("permanent DNS failure", &net.DNSError{IsNotFound: true}, true, 1),
		Entry("certificate failure", &url.Error{Op: "Post", URL: testAuthEndpoint, Err: x509.UnknownAuthorityError{}}, true, 1),
		Entry("signing or cookie storage timeout", context.DeadlineExceeded, false, 1),
		Entry("local EOF", io.EOF, false, 1),
		Entry("other transport failure", errors.New("unrecoverable transport error"), true, 1),
	)

	It("shares the attempt budget with HTTP failures and still honors Retry-After", func() {
		ctrl := gomock.NewController(GinkgoT())
		client := apphttp.NewMockClient[loginResult](ctrl)
		last := &apphttp.TransportError{Err: context.DeadlineExceeded}
		gomock.InOrder(
			client.EXPECT().Send(gomock.Any()).Return(apphttp.Result[loginResult]{}, &apphttp.TransportError{Err: io.EOF}),
			client.EXPECT().Send(gomock.Any()).Return(apphttp.Result[loginResult]{}, &apphttp.UnexpectedResponseError{StatusCode: 503, RetryAfter: "2"}),
			client.EXPECT().Send(gomock.Any()).Return(apphttp.Result[loginResult]{}, last),
		)
		var waits []time.Duration
		sut := &appstore{loginClient: client, authRetrySleep: func(delay time.Duration) { waits = append(waits, delay) }}
		_, err := sut.sendAuthenticationRequest(apphttp.Request{})
		Expect(errors.Is(err, last)).To(BeTrue())
		Expect(waits).To(Equal([]time.Duration{10 * time.Second, 2 * time.Second}))
		Expect(err.Error()).To(ContainSubstring("after 3 attempts (transport error, HTTP 503, transport error)"))
	})

	It("stops when an incomplete response requests a wait beyond the budget", func() {
		ctrl := gomock.NewController(GinkgoT())
		client := apphttp.NewMockClient[loginResult](ctrl)
		failure := &apphttp.TransportError{Err: context.DeadlineExceeded, RetryAfter: "3600"}
		client.EXPECT().Send(gomock.Any()).Return(apphttp.Result[loginResult]{}, failure).Times(1)
		sut := &appstore{loginClient: client, authRetrySleep: func(time.Duration) { Fail("must not sleep") }}
		_, err := sut.sendAuthenticationRequest(apphttp.Request{})
		Expect(errors.Is(err, failure)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("apple requested a wait longer than 30s"))
	})

	DescribeTable("honors Retry-After without exceeding the wait budget",
		func(status int, header string, calls int, expectedWaits []time.Duration) {
			ctrl := gomock.NewController(GinkgoT())
			client := apphttp.NewMockClient[loginResult](ctrl)
			responseErr := &apphttp.UnexpectedResponseError{StatusCode: status, RetryAfter: header}
			client.EXPECT().Send(gomock.Any()).Return(apphttp.Result[loginResult]{}, responseErr).Times(calls)
			var waits []time.Duration
			sut := &appstore{loginClient: client, authRetrySleep: func(delay time.Duration) { waits = append(waits, delay) }}
			_, err := sut.sendAuthenticationRequest(apphttp.Request{})
			Expect(errors.Is(err, responseErr)).To(BeTrue())
			if calls == 1 {
				Expect(waits).To(BeEmpty())
			} else {
				Expect(waits).To(Equal(expectedWaits))
			}
		},
		Entry("503 with a requested delay", 503, "5", 3, []time.Duration{5 * time.Second, 5 * time.Second}),
		Entry("429 with a requested delay", 429, "3", 3, []time.Duration{3 * time.Second, 3 * time.Second}),
		Entry("429 without a deadline", 429, "", 3, []time.Duration{10 * time.Second, 20 * time.Second}),
		Entry("429 with an invalid deadline", 429, "invalid", 3, []time.Duration{10 * time.Second, 20 * time.Second}),
		Entry("503 without a deadline", 503, "", 3, []time.Duration{10 * time.Second, 20 * time.Second}),
		Entry("204 without a deadline", 204, "", 3, []time.Duration{10 * time.Second, 20 * time.Second}),
		Entry("404 without a deadline", 404, "", 3, []time.Duration{10 * time.Second, 20 * time.Second}),
		Entry("Retry-After zero avoids a tight retry loop", 429, "0", 3, []time.Duration{time.Second, time.Second}),
		Entry("503 with a long deadline", 503, "3600", 1, []time.Duration(nil)),
		Entry("403 is not retried", 403, "1", 1, []time.Duration(nil)),
		Entry("malformed redirect is not retried", 302, "", 1, []time.Duration(nil)),
	)

	It("parses dates and seconds without overflow or negative waits", func() {
		now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
		for value, want := range map[string]time.Duration{
			now.Add(5 * time.Second).Format(http.TimeFormat): 5 * time.Second,
			now.Add(-time.Second).Format(http.TimeFormat):    0,
			"0": 0, " 5 ": 5 * time.Second,
			"18446744073709551615": maxAuthenticationRetryDelay + time.Second,
		} {
			delay, ok := authenticationRetryAfter(value, now)
			Expect(ok).To(BeTrue())
			Expect(delay).To(Equal(want))
		}
		for _, value := range []string{"", "-1", "invalid"} {
			_, ok := authenticationRetryAfter(value, now)
			Expect(ok).To(BeFalse())
		}
	})

	It("does not retry populated Apple credential errors or 2FA challenges", func() {
		ctrl := gomock.NewController(GinkgoT())
		client := apphttp.NewMockClient[loginResult](ctrl)
		sut := &appstore{loginClient: client, authRetrySleep: func(time.Duration) { Fail("must not sleep") }}
		for _, data := range []loginResult{
			{FailureType: FailureTypeInvalidCredentials, CustomerMessage: "invalid credentials"},
			{CustomerMessage: CustomerMessageBadLogin},
		} {
			client.EXPECT().Send(gomock.Any()).Return(apphttp.Result[loginResult]{StatusCode: 200, Data: data}, nil).Times(1)
			result, err := sut.sendAuthenticationRequest(apphttp.Request{})
			Expect(err).NotTo(HaveOccurred())
			_, _, err = sut.parseLoginResponse(&result, 2, "", testAuthEndpoint)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).NotTo(ContainSubstring("another network"))
		}
	})
})

// The adapter only redirects network traffic to the local server; login still
// validates Apple's advertised pod URL before this adapter receives a request.
type loginSessionJar struct{ *cookiejar.Jar }

func (loginSessionJar) Save() error { return nil }

var _ = Describe("Login session continuity", func() {
	It("recovers from a body timeout with cookies and a newly signed identical payload", func() {
		jar, err := cookiejar.New(nil)
		Expect(err).NotTo(HaveOccurred())
		client := apphttp.NewClient[loginResult](apphttp.Args{CookieJar: loginSessionJar{jar}, Authentication: true, Timeout: 250 * time.Millisecond})
		var calls atomic.Int32
		bodies := make(chan []byte, 2)
		releaseResponse := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer GinkgoRecover()
			call := calls.Add(1)
			Expect(r.Method).To(Equal(http.MethodPost))
			body, err := io.ReadAll(r.Body)
			Expect(err).NotTo(HaveOccurred())
			bodies <- body
			signature, err := base64.StdEncoding.DecodeString(r.Header.Get(apphttp.HeaderAppleActionSignature))
			Expect(err).NotTo(HaveOccurred())
			Expect(signature).To(Equal(body))
			if call == 1 {
				http.SetCookie(w, &http.Cookie{Name: "session", Value: "retained", Path: "/"})
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(http.StatusTooManyRequests)
				_, err = w.Write([]byte("<dict>"))
				Expect(err).NotTo(HaveOccurred())
				w.(http.Flusher).Flush()
				<-releaseResponse

				return
			}
			Expect(call).To(Equal(int32(2)))
			cookie, err := r.Cookie("session")
			Expect(err).NotTo(HaveOccurred())
			Expect(cookie.Value).To(Equal("retained"))
			_, err = w.Write([]byte("<dict><key>passwordToken</key><string>token</string><key>dsPersonId</key><string>123</string></dict>"))
			Expect(err).NotTo(HaveOccurred())
		}))
		DeferCleanup(srv.Close)
		DeferCleanup(func() { close(releaseResponse) })
		var waits []time.Duration
		sut := &appstore{loginClient: client, authRetrySleep: func(delay time.Duration) { waits = append(waits, delay) }}
		signer := &stubActionSigner{}
		request := sut.loginRequest("email", "password", "123456", "guid", srv.URL+PrivateAppStoreAPIPathAuth, 1, signer)
		result, err := sut.sendAuthenticationRequest(request)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Data.PasswordToken).To(Equal("token"))
		Expect(waits).To(Equal([]time.Duration{2 * time.Second}))
		Expect(signer.signCalls).To(Equal(2))
		Expect(<-bodies).To(Equal(<-bodies))
	})

	It("retains cookies and the signed payload through retries, a pod redirect and 2FA", func() {
		ctrl := gomock.NewController(GinkgoT())
		jar, err := cookiejar.New(nil)
		Expect(err).NotTo(HaveOccurred())
		client := apphttp.NewClient[loginResult](apphttp.Args{CookieJar: loginSessionJar{jar}, Authentication: true})
		adapter := apphttp.NewMockClient[loginResult](ctrl)
		keychain := keychain.NewMockKeychain(ctrl)
		keychain.EXPECT().Set("account", gomock.Any()).Return(nil).Times(1)
		sut := &appstore{loginClient: adapter, keychain: keychain, authRetrySleep: func(time.Duration) {}}
		connections := make(chan string, 4)
		calls := 0
		podURL := "https://p7-buy.itunes.apple.com" + PrivateAppStoreAPIPathAuth
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer GinkgoRecover()
			calls++
			connections <- r.RemoteAddr
			Expect(r.Method).To(Equal(http.MethodPost))
			body, err := io.ReadAll(r.Body)
			Expect(err).NotTo(HaveOccurred())
			signature, err := base64.StdEncoding.DecodeString(r.Header.Get(apphttp.HeaderAppleActionSignature))
			Expect(err).NotTo(HaveOccurred())
			Expect(signature).To(Equal(body))
			var payload map[string]string
			_, err = plist.Unmarshal(body, &payload)
			Expect(err).NotTo(HaveOccurred())
			Expect(payload["attempt"]).To(Equal("1"))
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
				w.Header().Set("Location", podURL)
				w.WriteHeader(http.StatusFound)
			case 3:
				Expect(payload["password"]).To(Equal("password"))
				data, err := plist.Marshal(map[string]string{"customerMessage": CustomerMessageBadLogin}, plist.XMLFormat)
				Expect(err).NotTo(HaveOccurred())
				_, err = w.Write(data)
				Expect(err).NotTo(HaveOccurred())
			case 4:
				Expect(payload["password"]).To(Equal("password123456"))
				w.Header().Set(HTTPHeaderStoreFront, "143441-1,29")
				_, err = w.Write([]byte("<dict><key>passwordToken</key><string>token</string><key>dsPersonId</key><string>123</string></dict>"))
				Expect(err).NotTo(HaveOccurred())
			default:
				Fail("unexpected request")
			}
		}))
		DeferCleanup(srv.Close)
		adapted := 0
		adapter.EXPECT().Send(gomock.Any()).DoAndReturn(func(request apphttp.Request) (apphttp.Result[loginResult], error) {
			adapted++
			if adapted == 3 {
				Expect(request.URL).To(Equal(podURL))
			}
			request.URL = srv.URL + PrivateAppStoreAPIPathAuth

			return client.Send(request)
		}).Times(4)
		signer := &stubActionSigner{}
		_, err = sut.login("email", "password", "", "guid", testAuthEndpoint, signer)
		Expect(errors.Is(err, ErrAuthCodeRequired)).To(BeTrue())
		account, err := sut.login("email", "password", "123456", "guid", testAuthEndpoint, signer)
		Expect(err).NotTo(HaveOccurred())
		Expect(account.PasswordToken).To(Equal("token"))
		seen := map[string]bool{}
		for range 4 {
			seen[<-connections] = true
		}
		Expect(seen).To(HaveLen(4))
	})
})
