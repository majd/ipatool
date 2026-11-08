package appstore

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Store AMD endpoint binding", func() {
	DescribeTable("origin-binds Store AMD proofs",
		func(endpoint string, allowed bool) {
			Expect(isStoreAMDCommerceURL(endpoint)).To(Equal(allowed))
		},
		Entry("volume Store", "https://buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/volumeStoreDownloadProduct?guid=001122334455", true),
		Entry("pod volume Store", "https://p42-buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/volumeStoreDownloadProduct?guid=001122334455", true),
		Entry("ent download", "https://downloaddispatch.itunes.apple.com/WebObjects/DownloadDispatch.woa/wa/ent/download?guid=001122334455", true),
		Entry("redownload", "https://downloaddispatch.itunes.apple.com/r/redownload?guid=001122334455", true),
		Entry("update", "https://downloaddispatch.itunes.apple.com/up/updateProduct?guid=001122334455", true),
		Entry("foreign host", "https://example.test/WebObjects/MZFinance.woa/wa/volumeStoreDownloadProduct?guid=001122334455", false),
		Entry("host suffix spoof", "https://p42-buy.itunes.apple.com.example.test/WebObjects/MZFinance.woa/wa/volumeStoreDownloadProduct?guid=001122334455", false),
		Entry("nonnumeric pod", "https://evil-buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/volumeStoreDownloadProduct?guid=001122334455", false),
		Entry("insecure scheme", "http://buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/volumeStoreDownloadProduct?guid=001122334455", false),
		Entry("userinfo", "https://user@buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/volumeStoreDownloadProduct?guid=001122334455", false),
		Entry("unsupported port", "https://buy.itunes.apple.com:8443/WebObjects/MZFinance.woa/wa/volumeStoreDownloadProduct?guid=001122334455", false),
		Entry("encoded path", "https://downloaddispatch.itunes.apple.com/r/%72edownload?guid=001122334455", false),
		Entry("fragment", "https://downloaddispatch.itunes.apple.com/r/redownload?guid=001122334455#fragment", false),
		Entry("unexpected query", "https://downloaddispatch.itunes.apple.com/r/redownload?token=secret", false),
		Entry("unknown path", "https://downloaddispatch.itunes.apple.com/other?guid=001122334455", false),
	)
})
