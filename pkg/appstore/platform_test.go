package appstore

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Platform", func() {
	DescribeTable("parses aliases",
		func(value string, expected Platform) {
			platform, err := ParsePlatform(value)
			Expect(err).ToNot(HaveOccurred())
			Expect(platform).To(Equal(expected))
		},
		Entry("default", "", Platform("")),
		Entry("iPhone", "iphone", PlatformIPhone),
		Entry("iOS", "ios", PlatformIPhone),
		Entry("iPad", "ipad", PlatformIPad),
		Entry("AppleTV", "appletv", PlatformAppleTV),
		Entry("tvOS", "tvos", PlatformAppleTV),
	)

	It("returns an error for invalid platforms", func() {
		_, err := ParsePlatform("watch")
		Expect(err).To(HaveOccurred())
	})
})
