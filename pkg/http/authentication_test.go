package http

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Authentication responses", func() {
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
		Entry("permanent redirect without Location", 301, "secret-token", "authentication redirect is missing Location"),
		Entry("temporary redirect without Location", 307, "secret-token", "authentication redirect is missing Location"),
		Entry("permanent POST redirect without Location", 308, "secret-token", "authentication redirect is missing Location"),
		Entry("rate limit", 429, "secret-token", "rate limited by Apple"),
		Entry("malformed plist", 200, "<plist><dict>secret-token", "malformed authentication plist"),
	)
})
