package assets

import (
	"archive/zip"

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
			fileSpec{name: "adid", size: 9797952, digest: mustDigest("1fec225b6917206e3d9b00fe636c04878f82bfd06a87a4b6941711de60efab5b")},
			fileSpec{name: "CoreFP", size: 94842464, digest: mustDigest("2c30fa2b5f695fd66c0360dce72f54757d03ddb7107549715c12cb359a003b46")},
			fileSpec{name: "CoreFP.icxs", size: 7365344, digest: mustDigest("cb8f55330ec567da3e692dab5f5388bed0312dd9093340477c5759078cc2aa7b")},
			fileSpec{name: "fairplayd", size: 37689264, digest: mustDigest("28b4c7b2a5a0409c5020da55fc3984713a8e2b0eec8557076c8717f7166d003e")},
			fileSpec{name: "FairPlayIOKit", size: 2132320, digest: mustDigest("47452960ec010d3a80b02cceec73a65f8a9758850fd0fb7b2fdac7a5d1b81025")},
		))
	})

	It("pins the CoreADI path and deflated kernel entry", func() {
		Expect(frameworkImagePaths).To(ContainElement("/System/Library/PrivateFrameworks/CoreADI.framework/Versions/A/adid"))
		archive := &zip.Reader{File: []*zip.File{{FileHeader: zip.FileHeader{
			Name: kernelCacheName, Method: zip.Deflate,
			CompressedSize64: kernelCacheCompressedSize, UncompressedSize64: kernelCacheArchiveSize,
		}}}}
		_, err := pinnedZipEntry(archive, kernelCacheName, kernelCacheCompressedSize, kernelCacheArchiveSize)
		Expect(err).NotTo(HaveOccurred())
	})

	It("rejects an incomplete bundle", func() {
		Expect(validate(Bundle{})).To(MatchError(ContainSubstring("AppleMediaServices has size 0")))
	})
})
