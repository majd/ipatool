package assets

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"

	"github.com/blacktop/go-macho/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Runtime bundle compaction", func() {
	It("retains independent arm64e images and preserves thin images and ICXS", func() {
		fat := make([]byte, 32)
		for i, value := range []uint32{types.MagicFat.Int(), 1, uint32(types.CPUArm64), uint32(types.CPUSubtypeArm64E), 28, 4, 0} {
			binary.BigEndian.PutUint32(fat[i*4:], value)
		}
		copy(fat[28:], "arm!")
		thin, icxs := []byte("thin"), []byte("ICXS")
		bundle := Bundle{CoreFP: fat, Commerce: fat, ADID: fat, FairPlayD: fat, AppleMediaServices: thin, FairPlayKext: thin, CoreFPICXS: icxs}
		original := bytes.Clone(fat)
		compact, err := compactBundle(bundle)
		Expect(err).NotTo(HaveOccurred())
		for _, data := range [][]byte{compact.CoreFP, compact.Commerce, compact.ADID, compact.FairPlayD} {
			Expect(string(data)).To(Equal("arm!"))
			Expect(&data[0]).NotTo(BeIdenticalTo(&fat[28]))
			Expect(cap(data)).To(BeNumerically("<", len(fat)))
		}
		Expect(&compact.AppleMediaServices[0]).To(BeIdenticalTo(&thin[0]))
		Expect(&compact.FairPlayKext[0]).To(BeIdenticalTo(&thin[0]))
		Expect(&compact.CoreFPICXS[0]).To(BeIdenticalTo(&icxs[0]))
		Expect(fat).To(Equal(original))
		Expect(bundle.CoreFP).To(HaveLen(32))
	})
	It("verifies the entire cached file before discarding unused architectures", func() {
		fat := make([]byte, 40)
		for i, value := range []uint32{types.MagicFat.Int(), 1, uint32(types.CPUArm64), uint32(types.CPUSubtypeArm64E), 28, 4, 0} {
			binary.BigEndian.PutUint32(fat[i*4:], value)
		}
		copy(fat[28:], "arm!unused!!")
		originalSpecs := requiredFiles
		DeferCleanup(func() { requiredFiles = originalSpecs })
		requiredFiles = []fileSpec{{name: "CoreFP", size: len(fat), digest: sha256.Sum256(fat)}}
		directory := GinkgoT().TempDir()
		path := filepath.Join(directory, "CoreFP")
		Expect(os.WriteFile(path, fat, 0o600)).To(Succeed())
		bundle, err := readCache(directory)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(bundle.CoreFP)).To(Equal("arm!"))
		cached, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(cached).To(Equal(fat))
		fat[len(fat)-1] ^= 0xff
		Expect(os.WriteFile(path, fat, 0o600)).To(Succeed())
		_, err = readCache(directory)
		Expect(err).To(MatchError(ContainSubstring("failed integrity verification")))
	})

	It("rejects invalid universal images", func() {
		fat := make([]byte, 4)
		binary.BigEndian.PutUint32(fat, types.MagicFat.Int())
		_, err := compactBundle(Bundle{CoreFP: fat})
		Expect(err).To(MatchError(ContainSubstring("select CoreFP architecture")))
	})
})
