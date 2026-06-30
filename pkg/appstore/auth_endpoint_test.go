package appstore

import (
	"errors"

	"github.com/majd/ipatool/v2/pkg/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Auth endpoint", func() {
	It("falls back to the native auth endpoint", func() {
		Expect(normalizeAuthEndpoint()).To(Equal(defaultNativeAuthEndpoint))
	})

	It("normalizes Apple's native auth endpoint with the fast path and trailing slash", func() {
		Expect(normalizeAuthEndpoint("https://auth.itunes.apple.com/auth/v1/native")).
			To(Equal(defaultNativeAuthEndpoint))
		Expect(normalizeAuthEndpoint("https://auth.itunes.apple.com/auth/v1/native/fast")).
			To(Equal(defaultNativeAuthEndpoint))
	})

	It("keeps legacy endpoints unchanged", func() {
		endpoint := "https://buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/authenticate"
		Expect(normalizeAuthEndpoint(endpoint)).To(Equal(endpoint))
	})

	It("prefers the first non-empty endpoint", func() {
		Expect(normalizeAuthEndpoint(
			"https://auth.itunes.apple.com/auth/v1/native",
			"https://buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/authenticate",
		)).To(Equal(defaultNativeAuthEndpoint))
	})

	It("extracts a native endpoint from an escaped response body", func() {
		body := `{"authenticateAccount":"https:\/\/auth.itunes.apple.com\/auth\/v1\/native"}`
		Expect(authEndpointFromText(body)).To(Equal(defaultNativeAuthEndpoint))
	})

	It("extracts a native endpoint from a response decode error", func() {
		err := &http.ResponseDecodeError{
			Cause: errors.New("decode failed"),
			URLs:  []string{"https://auth.itunes.apple.com/auth/v1/native"},
		}
		Expect(authEndpointFromResponseError(err)).To(Equal(defaultNativeAuthEndpoint))
	})

	It("does not mutate URLs on the decode error", func() {
		urls := []string{"https://auth.itunes.apple.com/auth/v1/native"}
		err := &http.ResponseDecodeError{
			Cause: errors.New("decode failed"),
			URLs:  urls,
			Body:  "ignored",
		}
		_ = authEndpointFromResponseError(err)
		Expect(urls).To(Equal([]string{"https://auth.itunes.apple.com/auth/v1/native"}))
	})
})
