package appstore

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Download package platform", func() {
	It("uses the requested platform for native macOS packages", func() {
		item := downloadItemResult{
			Metadata: map[string]interface{}{
				"software-platform": "macos",
				"product-type":      "mac-os-app",
			},
			Sinfs: []Sinf{{DPInfo: []byte("dp info")}},
		}

		platform, err := downloadPackagePlatform(PlatformMacOS, item)
		Expect(err).ToNot(HaveOccurred())
		Expect(platform).To(Equal(PlatformMacOS))
	})

	It("accepts native authorization data that also contains sinf data", func() {
		item := downloadItemResult{
			Metadata: map[string]interface{}{
				"software-platform": "macos",
				"product-type":      "mac-os-app",
			},
			Sinfs: []Sinf{{Data: []byte("sinf"), DPInfo: []byte("dp info")}},
		}

		platform, err := downloadPackagePlatform(PlatformMacOS, item)
		Expect(err).ToNot(HaveOccurred())
		Expect(platform).To(Equal(PlatformMacOS))
	})

	It("recognizes iOS apps made available on macOS", func() {
		item := downloadItemResult{
			Metadata: map[string]interface{}{
				"software-platform": "ios",
				"product-type":      "ios-app",
			},
			Sinfs: []Sinf{{Data: []byte("sinf")}},
		}

		platform, err := downloadPackagePlatform(PlatformMacOS, item)
		Expect(err).ToNot(HaveOccurred())
		Expect(platform).To(Equal(PlatformIPhone))
	})

	It("uses mobile sinf data when platform metadata is absent", func() {
		platform, err := downloadPackagePlatform(PlatformMacOS, downloadItemResult{
			Sinfs: []Sinf{{Data: []byte("sinf")}},
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(platform).To(Equal(PlatformIPhone))
	})

	It("rejects mobile metadata without usable sinf data", func() {
		_, err := downloadPackagePlatform(PlatformMacOS, downloadItemResult{
			Metadata: map[string]interface{}{"software-platform": "ios"},
		})

		Expect(err).To(MatchError(ContainSubstring("mobile sinf")))
	})

	It("rejects conflicting platform metadata", func() {
		_, err := downloadPackagePlatform(PlatformMacOS, downloadItemResult{
			Metadata: map[string]interface{}{
				"software-platform": "macos",
				"product-type":      "ios-app",
			},
			Sinfs: []Sinf{{Data: []byte("sinf")}},
		})

		Expect(err).To(MatchError(ContainSubstring("conflicting")))
	})

	It("rejects conflicting authorization data", func() {
		_, err := downloadPackagePlatform(PlatformMacOS, downloadItemResult{
			Sinfs: []Sinf{
				{Data: []byte("sinf")},
				{DPInfo: []byte("dp info")},
			},
		})

		Expect(err).To(MatchError(ContainSubstring("conflicting")))
	})

	It("does not override non-macOS requests", func() {
		platform, err := downloadPackagePlatform(PlatformIPad, downloadItemResult{
			Metadata: map[string]interface{}{
				"software-platform": "macos",
				"product-type":      "mac-os-app",
			},
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(platform).To(Equal(PlatformIPad))
	})
})
