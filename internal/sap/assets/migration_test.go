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

var _ = Describe("Compact asset cache migration", func() {
	var directory, legacyDirectory string
	var source, selected []byte
	BeforeEach(func() {
		root := GinkgoT().TempDir()
		directory = filepath.Join(root, assetCacheDirectory)
		legacyDirectory = filepath.Join(root, legacyAssetCacheDirectory)
		source = make([]byte, 40)
		for n, v := range []uint32{types.MagicFat.Int(), 1, uint32(types.CPUArm64), uint32(types.CPUSubtypeArm64E), 28, 4, 0} {
			binary.BigEndian.PutUint32(source[n*4:], v)
		}
		copy(source[28:], "arm!unused!")
		selected = []byte("arm!")
		originalRequired, originalLegacy := requiredFiles, legacyFiles
		DeferCleanup(func() { requiredFiles, legacyFiles = originalRequired, originalLegacy })
		requiredFiles = []fileSpec{{name: "CoreFP", size: len(selected), digest: sha256.Sum256(selected)}}
		legacyFiles = []fileSpec{{name: "CoreFP", size: len(source), digest: sha256.Sum256(source)}}
		Expect(os.MkdirAll(legacyDirectory, 0o700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(legacyDirectory, "CoreFP"), source, 0o600)).To(Succeed())
	})
	It("verifies legacy files and writes only the selected architecture", func() {
		bundle, found := loadCachedBundle(directory)
		Expect(found).To(BeTrue())
		Expect(bundle.CoreFP).To(Equal(selected))
		migrated, err := os.ReadFile(filepath.Join(directory, "CoreFP"))
		Expect(err).NotTo(HaveOccurred())
		Expect(migrated).To(Equal(selected))
		old, err := os.ReadFile(filepath.Join(legacyDirectory, "CoreFP"))
		Expect(err).NotTo(HaveOccurred())
		Expect(old).To(Equal(source))
	})
	It("uses a compact cache without requiring the old files", func() {
		Expect(writeCache(directory, Bundle{CoreFP: selected})).To(Succeed())
		Expect(os.RemoveAll(legacyDirectory)).To(Succeed())
		bundle, found := loadCachedBundle(directory)
		Expect(found).To(BeTrue())
		Expect(bundle.CoreFP).To(Equal(selected))
	})
	It("rejects corrupt discarded legacy bytes", func() {
		corrupt := bytes.Clone(source)
		corrupt[len(corrupt)-1] ^= 0xff
		Expect(os.WriteFile(filepath.Join(legacyDirectory, "CoreFP"), corrupt, 0o600)).To(Succeed())
		_, found := loadCachedBundle(directory)
		Expect(found).To(BeFalse())
	})
	It("repairs corrupt compact data from a valid old cache", func() {
		Expect(os.MkdirAll(directory, 0o700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(directory, "CoreFP"), []byte("bad!"), 0o600)).To(Succeed())
		bundle, found := loadCachedBundle(directory)
		Expect(found).To(BeTrue())
		Expect(bundle.CoreFP).To(Equal(selected))
	})
	It("keeps valid legacy data usable when the compact cache cannot be written", func() {
		Expect(os.WriteFile(directory, []byte("not a directory"), 0o600)).To(Succeed())
		bundle, found := loadCachedBundle(directory)
		Expect(found).To(BeTrue())
		Expect(bundle.CoreFP).To(Equal(selected))
	})
	It("rejects a legacy slice that does not match the new pin", func() {
		requiredFiles[0].digest = sha256.Sum256([]byte("bad!"))
		_, found := loadCachedBundle(directory)
		Expect(found).To(BeFalse())
	})

})
