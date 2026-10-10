package assets

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("macOS 27 asset profile", func() {
	It("pins the Apple restore image and cache namespace", func() {
		Expect(macOSVersion).To(Equal("27.0.1"))
		Expect(macOSBuild).To(Equal("26A434"))
		Expect(macOSRestoreURL).To(Equal("https://updates.cdn-apple.com/2026FallFCS/59241290-5d51-4ca8-9df4-31624b9a4eac/UniversalMac_27.0.1_26A434_Restore.ipsw"))
		Expect(assetCacheDirectory).To(ContainSubstring("macos-27-26A434"))
	})

	It("pins every runtime artifact by size and digest", func() {
		Expect(requiredFiles).To(ConsistOf(
			fileSpec{name: "AppleMediaServices", size: 14813888, digest: mustDigest("95f4558aa2c0ccd9f96ed479e336b630b46bf4b4e0baabc1c1ac8c7d55e21ab4")},
			fileSpec{name: "commerce", size: 8092640, digest: mustDigest("76de40a4a32f691947cb926ce3b128d2fc446f7cac9755f831c406cf5a316ac9")},
			fileSpec{name: "CoreFP", size: 94842464, digest: mustDigest("2c30fa2b5f695fd66c0360dce72f54757d03ddb7107549715c12cb359a003b46")},
			fileSpec{name: "CoreFP.icxs", size: 7365344, digest: mustDigest("cb8f55330ec567da3e692dab5f5388bed0312dd9093340477c5759078cc2aa7b")},
		))
	})

	It("rejects an incomplete bundle", func() {
		Expect(validate(Bundle{})).To(MatchError(ContainSubstring("AppleMediaServices has size 0")))
	})
})
