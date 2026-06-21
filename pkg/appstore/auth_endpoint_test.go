package appstore

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Auth endpoint", func() {
	It("falls back to the current native auth endpoint", func() {
		Expect(normalizeAuthEndpoint()).To(Equal("https://auth.itunes.apple.com/auth/v1/native/fast/"))
	})

	It("normalizes native auth endpoints with the fast path and trailing slash", func() {
		Expect(normalizeAuthEndpoint("https://auth.itunes.apple.com/auth/v1/native")).
			To(Equal("https://auth.itunes.apple.com/auth/v1/native/fast/"))
		Expect(normalizeAuthEndpoint("https://auth.itunes.apple.com/auth/v1/native/fast")).
			To(Equal("https://auth.itunes.apple.com/auth/v1/native/fast/"))
		Expect(normalizeAuthEndpoint("https://auth.itunes.apple.com/auth/v1/native/fast/")).
			To(Equal("https://auth.itunes.apple.com/auth/v1/native/fast/"))
	})

	It("keeps legacy auth endpoints unchanged", func() {
		endpoint := "https://buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/authenticate"
		Expect(normalizeAuthEndpoint(endpoint)).To(Equal(endpoint))
	})

	It("uses the first non-empty endpoint", func() {
		Expect(normalizeAuthEndpoint("", "https://auth.itunes.apple.com/auth/v1/native")).
			To(Equal("https://auth.itunes.apple.com/auth/v1/native/fast/"))
	})
})
