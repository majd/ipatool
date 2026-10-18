package appstore

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"

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
		Entry("request deadline wrapped by the framework transport", &url.Error{Op: "Post", URL: testAuthEndpoint, Err: fmt.Errorf("failed to make round trip: %w", context.DeadlineExceeded)}, true, 3),
		Entry("wrapped DNS timeout", &url.Error{Op: "Post", URL: testAuthEndpoint, Err: &net.DNSError{IsTimeout: true}}, true, 3),
		Entry("DNS timeout wrapped by the framework transport", &url.Error{Op: "Post", URL: testAuthEndpoint, Err: fmt.Errorf("failed to make round trip: %w", &net.DNSError{IsTimeout: true})}, true, 3),
		Entry("connection closed", io.EOF, true, 3),
		Entry("truncated response body", io.ErrUnexpectedEOF, true, 3),
		Entry("connection reset", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, true, 3),
		Entry("broken pipe", &net.OpError{Op: "write", Net: "tcp", Err: syscall.EPIPE}, true, 3),
		Entry("canceled request", context.Canceled, true, 1),
		Entry("canceled request wrapped by the framework transport", &url.Error{Op: "Post", URL: testAuthEndpoint, Err: fmt.Errorf("failed to make round trip: %w", context.Canceled)}, true, 1),
		Entry("permanent DNS failure", &net.DNSError{IsNotFound: true}, true, 1),
		Entry("certificate failure", &url.Error{Op: "Post", URL: testAuthEndpoint, Err: x509.UnknownAuthorityError{}}, true, 1),
		Entry("signing or cookie storage timeout", context.DeadlineExceeded, false, 1),
		Entry("local EOF", io.EOF, false, 1),
		Entry("other transport failure", errors.New("unrecoverable transport error"), true, 1),
	)

	DescribeTable("shares the attempt budget with HTTP failures and still honors Retry-After", func(status int) {
		ctrl := gomock.NewController(GinkgoT())
		client := apphttp.NewMockClient[loginResult](ctrl)
		last := &apphttp.TransportError{Err: context.DeadlineExceeded}
		gomock.InOrder(
			client.EXPECT().Send(gomock.Any()).Return(apphttp.Result[loginResult]{}, &apphttp.TransportError{Err: io.EOF}),
			client.EXPECT().Send(gomock.Any()).Return(apphttp.Result[loginResult]{}, &apphttp.UnexpectedResponseError{StatusCode: status, RetryAfter: "2"}),
			client.EXPECT().Send(gomock.Any()).Return(apphttp.Result[loginResult]{}, last),
		)
		var waits []time.Duration
		sut := &appstore{loginClient: client, authRetrySleep: func(delay time.Duration) { waits = append(waits, delay) }}
		_, err := sut.sendAuthenticationRequest(apphttp.Request{})
		Expect(errors.Is(err, last)).To(BeTrue())
		Expect(waits).To(Equal([]time.Duration{10 * time.Second, 2 * time.Second}))
		Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("after 3 attempts (transport error, HTTP %d, transport error)", status)))
	},
		Entry("non-plist 403", http.StatusForbidden),
		Entry("503", http.StatusServiceUnavailable),
	)

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
		Entry("403 without a deadline", 403, "", 3, []time.Duration{10 * time.Second, 20 * time.Second}),
		Entry("403 with a requested delay", 403, "1", 3, []time.Duration{time.Second, time.Second}),
		Entry("403 with a long deadline", 403, "3600", 1, []time.Duration(nil)),
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

	DescribeTable("does not retry populated Apple credential errors or 2FA challenges", func(status int) {
		ctrl := gomock.NewController(GinkgoT())
		client := apphttp.NewMockClient[loginResult](ctrl)
		sut := &appstore{loginClient: client, authRetrySleep: func(time.Duration) { Fail("must not sleep") }}
		for _, data := range []loginResult{
			{FailureType: FailureTypeInvalidCredentials, CustomerMessage: "invalid credentials"},
			{CustomerMessage: CustomerMessageBadLogin},
		} {
			client.EXPECT().Send(gomock.Any()).Return(apphttp.Result[loginResult]{StatusCode: status, Data: data}, nil).Times(1)
			result, err := sut.sendAuthenticationRequest(apphttp.Request{})
			Expect(err).NotTo(HaveOccurred())
			_, _, err = sut.parseLoginResponse(&result, 2, "", testAuthEndpoint)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).NotTo(ContainSubstring("another network"))
		}
	},
		Entry("HTTP 200", http.StatusOK),
		Entry("HTTP 403", http.StatusForbidden),
	)
})

// The adapter only redirects network traffic to the local server; login still
// validates Apple's advertised pod URL before this adapter receives a request.
