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
		Entry("iPadOS", "iPadOS", PlatformIPad),
		Entry("AppleTV", "appletv", PlatformAppleTV),
		Entry("tvOS", "tvos", PlatformAppleTV),
		Entry("visionOS", "visionOS", PlatformVisionOS),
		Entry("Vision Pro", "visionpro", PlatformVisionOS),
		Entry("xrOS", "xrOS", PlatformVisionOS),
		Entry("realityDevice", "realityDevice", PlatformVisionOS),
		Entry("Mac", "mac", PlatformMacOS),
		Entry("macOS", "macOS", PlatformMacOS),
		Entry("OS X", "osx", PlatformMacOS),
	)

	DescribeTable("maps platforms to lookup entities",
		func(platform Platform, expected string) {
			entity, err := platform.lookupEntity()
			Expect(err).ToNot(HaveOccurred())
			Expect(entity).To(Equal(expected))
		},
		Entry("default", Platform(""), "software,iPadSoftware"),
		Entry("iPhone", PlatformIPhone, "software"),
		Entry("iPad", PlatformIPad, "iPadSoftware"),
		Entry("Apple TV", PlatformAppleTV, "tvSoftware"),
		Entry("visionOS", PlatformVisionOS, "xrosSoftware"),
		Entry("macOS", PlatformMacOS, "macSoftware"),
	)

	DescribeTable("maps platforms to search entities",
		func(platform Platform, expected string) {
			entity, err := platform.searchEntity()
			Expect(err).ToNot(HaveOccurred())
			Expect(entity).To(Equal(expected))
		},
		Entry("default", Platform(""), "software,iPadSoftware"),
		Entry("iPhone", PlatformIPhone, "software"),
		Entry("iPad", PlatformIPad, "iPadSoftware"),
		Entry("Apple TV", PlatformAppleTV, "software,tvSoftware"),
		Entry("visionOS", PlatformVisionOS, "xrosSoftware"),
		Entry("macOS", PlatformMacOS, "macSoftware"),
	)

	DescribeTable("maps platforms to metadata platforms",
		func(platform Platform, expected string) {
			metadataPlatform, err := platform.metadataPlatform()
			Expect(err).ToNot(HaveOccurred())
			Expect(metadataPlatform).To(Equal(expected))
		},
		Entry("iPhone", PlatformIPhone, "enterprisestore"),
		Entry("iPad", PlatformIPad, "enterprisestore"),
		Entry("Apple TV", PlatformAppleTV, "atv9"),
		Entry("visionOS", PlatformVisionOS, "realityDevice"),
	)

	It("returns an error for invalid platforms", func() {
		_, err := ParsePlatform("watch")
		Expect(err).To(HaveOccurred())
	})

	It("returns errors when an unknown platform is mapped", func() {
		platform := Platform("watch")

		_, lookupErr := platform.lookupEntity()
		_, searchErr := platform.searchEntity()
		_, metadataErr := platform.metadataPlatform()

		Expect(lookupErr).To(HaveOccurred())
		Expect(searchErr).To(HaveOccurred())
		Expect(metadataErr).To(HaveOccurred())
	})
})
