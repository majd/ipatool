package appstore

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AppStore (ZIP local metadata)", func() {
	// Local Unix timestamp field captured from the affected United Airlines IPA.
	unixTimestamp := []byte{0x55, 0x58, 12, 0, 0xb1, 0x24, 0x51, 0x6a, 0xb1, 0x24, 0x51, 0x6a, 0xcb, 0, 0xc8, 0}
	localTimestamp := []byte{0x55, 0x54, 9, 0, 3, 0xb2, 0x27, 0x51, 0x6a, 0xb2, 0x27, 0x51, 0x6a}
	centralTimestamp := []byte{0x55, 0x54, 5, 0, 1, 0xb2, 0x27, 0x51, 0x6a}
	DescribeTable("preserves independent extra fields through metadata and SINF patches",
		func(local, central []byte) {
			source := timestampZIP(local, central)
			dir, err := os.MkdirTemp("", "ipatool-zip-timestamps-*")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(os.RemoveAll, dir)
			original, patched := filepath.Join(dir, "original.ipa"), filepath.Join(dir, "patched.ipa")
			Expect(os.WriteFile(original, source, 0600)).To(Succeed())
			store := &appstore{os: operatingsystem.New()}
			Expect(store.applyPatches(downloadItemResult{Metadata: map[string]interface{}{}}, Account{}, original, patched, nil)).To(Succeed())
			checkTimestampZIP(patched, local, central)
			Expect(store.ReplicateSinf(ReplicateSinfInput{PackagePath: patched, Sinfs: []Sinf{{Data: []byte("sinf")}}})).To(Succeed())
			checkTimestampZIP(patched, local, central)
		},
		Entry("United Airlines local-only Unix timestamp", unixTimestamp, []byte{}),
		Entry("identical local and central timestamps", centralTimestamp, centralTimestamp),
		Entry("local-only extended timestamp", localTimestamp, []byte{}),
		Entry("different local and central timestamps", localTimestamp, centralTimestamp),
		Entry("central-only extended timestamp", []byte{}, centralTimestamp),
		Entry("no extended timestamps", []byte{}, []byte{}),
		Entry("unknown local field", []byte{0xfe, 0xca, 3, 0, 1, 2, 3}, []byte{}),
	)

	It("reads local extras in central-directory order, including prefixed archives", func() {
		data := timestampZIP(localTimestamp, nil)
		end := len(data) - 22
		start := int(binary.LittleEndian.Uint32(data[end+16:]))
		firstLen := 46 + int(binary.LittleEndian.Uint16(data[start+28:])) + int(binary.LittleEndian.Uint16(data[start+30:]))
		reordered := append([]byte{}, data[:start]...)
		reordered = append(reordered, data[start+firstLen:end]...)
		reordered = append(reordered, data[start:start+firstLen]...)
		reordered = append(reordered, data[end:]...)
		reordered = append([]byte("archive prefix"), reordered...)
		reader, err := zip.NewReader(bytes.NewReader(reordered), int64(len(reordered)))
		Expect(err).ToNot(HaveOccurred())
		headers, err := newZIPLocalHeaders(bytes.NewReader(reordered), int64(len(reordered)))
		Expect(err).ToNot(HaveOccurred())
		for _, file := range reader.File {
			extra, err := headers.extra(file)
			Expect(err).ToNot(HaveOccurred())
			if file.Name == "Payload/Test.app/_CodeSignature/CodeResources" {
				Expect(extra).To(Equal(localTimestamp))
			}
		}
	})

	It("reads ZIP64 entry offsets and a ZIP64 end record", func() {
		out := zip64TimestampZIP(localTimestamp)
		reader, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
		Expect(err).ToNot(HaveOccurred())
		headers, err := newZIPLocalHeaders(bytes.NewReader(out), int64(len(out)))
		Expect(err).ToNot(HaveOccurred())
		extra, err := headers.extra(reader.File[0])
		Expect(err).ToNot(HaveOccurred())
		Expect(extra).To(Equal(localTimestamp))

		By("rewriting the ZIP64 archive without retaining stale central offsets")
		dir, err := os.MkdirTemp("", "ipatool-zip64-*")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, dir)
		path := filepath.Join(dir, "source.zip")
		Expect(os.WriteFile(path, out, 0600)).To(Succeed())
		src, err := zip.OpenReader(path)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(src.Close)
		var output bytes.Buffer
		dst := zip.NewWriter(&output)
		Expect((&appstore{}).replicateZip(src, dst, path)).To(Succeed())
		Expect(dst.Close()).To(Succeed())
		resultPath := filepath.Join(dir, "result.zip")
		Expect(os.WriteFile(resultPath, output.Bytes(), 0600)).To(Succeed())
		checkTimestampZIP(resultPath, localTimestamp, []byte{})
	})

	It("replaces local ZIP64 placeholder sizes when removing a stored entry's descriptor", func() {
		extra := append([]byte{}, unixTimestamp...)
		placeholder := make([]byte, 20)
		binary.LittleEndian.PutUint16(placeholder, 1)
		binary.LittleEndian.PutUint16(placeholder[2:], 16)
		extra = append(extra, placeholder...)
		size := uint64(math.MaxUint32) + 10
		result := zipStoredExtra(&zip.FileHeader{Method: zip.Store, CompressedSize64: size, UncompressedSize64: size}, extra)
		Expect(result[:len(unixTimestamp)]).To(Equal(unixTimestamp))
		Expect(result).To(HaveLen(len(unixTimestamp) + 20))
		Expect(binary.LittleEndian.Uint64(result[len(unixTimestamp)+4:])).To(Equal(size))
		Expect(binary.LittleEndian.Uint64(result[len(unixTimestamp)+12:])).To(Equal(size))
	})

	It("rejects a truncated local extra field", func() {
		data := timestampZIP(localTimestamp, nil)
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		Expect(err).ToNot(HaveOccurred())
		binary.LittleEndian.PutUint16(data[28:], 0xffff)
		headers, err := newZIPLocalHeaders(bytes.NewReader(data), int64(len(data)))
		Expect(err).ToNot(HaveOccurred())
		_, err = headers.extra(reader.File[0])
		Expect(err).To(HaveOccurred())
	})
})

// Build distinct local/central metadata without using the production helper.
func timestampZIP(local, central []byte) []byte {
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	payload := []byte("signature")
	header := &zip.FileHeader{
		Name:               "Payload/Test.app/_CodeSignature/CodeResources",
		Method:             zip.Store,
		ModifiedDate:       0x5cea,
		ModifiedTime:       0x874d,
		Extra:              local,
		CRC32:              crc32.ChecksumIEEE(payload),
		CompressedSize64:   uint64(len(payload)),
		UncompressedSize64: uint64(len(payload)),
	}
	entry, err := writer.CreateRaw(header)
	Expect(err).ToNot(HaveOccurred())
	_, err = entry.Write(payload)
	Expect(err).ToNot(HaveOccurred())

	header.Extra = central
	info, err := writer.Create("Payload/Test.app/Info.plist")
	Expect(err).ToNot(HaveOccurred())
	_, err = io.WriteString(info, `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>Test</string></dict></plist>`)
	Expect(err).ToNot(HaveOccurred())
	Expect(writer.Close()).To(Succeed())

	return output.Bytes()
}

func checkTimestampZIP(path string, local, central []byte) {
	data, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred())
	// This fixture's CodeResources is the first entry. Inspect its local bytes
	// independently of the production reader and Go's central-only FileHeader.
	nameLen := int(binary.LittleEndian.Uint16(data[26:]))
	extraLen := int(binary.LittleEndian.Uint16(data[28:]))
	Expect(data[30+nameLen : 30+nameLen+extraLen]).To(Equal(local))
	Expect(binary.LittleEndian.Uint16(data[10:])).To(Equal(uint16(0x874d)))
	Expect(binary.LittleEndian.Uint16(data[12:])).To(Equal(uint16(0x5cea)))
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	Expect(err).ToNot(HaveOccurred())
	Expect(reader.File[0].Extra).To(BeEquivalentTo(central))
	entry, err := reader.File[0].Open()
	Expect(err).ToNot(HaveOccurred())
	payload, err := io.ReadAll(entry)
	Expect(err).ToNot(HaveOccurred())
	Expect(entry.Close()).To(Succeed())
	Expect(string(payload)).To(Equal("signature"))
}

// Force ZIP64 offsets and end records without allocating a multi-gigabyte fixture.
func zip64TimestampZIP(local []byte) []byte {
	data := timestampZIP(local, nil)
	end := len(data) - 22
	start := int(binary.LittleEndian.Uint32(data[end+16:]))
	nameLen := int(binary.LittleEndian.Uint16(data[start+28:]))
	offsetExtra := make([]byte, 12)
	binary.LittleEndian.PutUint16(offsetExtra, 1)
	binary.LittleEndian.PutUint16(offsetExtra[2:], 8)
	binary.LittleEndian.PutUint32(data[start+42:], 0xffffffff)
	binary.LittleEndian.PutUint16(data[start+30:], 12)
	out := append([]byte{}, data[:start+46+nameLen]...)
	out = append(out, offsetExtra...)
	out = append(out, data[start+46+nameLen:end]...)
	zip64Offset := len(out)
	end64 := make([]byte, 56)
	binary.LittleEndian.PutUint32(end64, 0x06064b50)
	binary.LittleEndian.PutUint64(end64[4:], 44)
	binary.LittleEndian.PutUint64(end64[24:], 2)
	binary.LittleEndian.PutUint64(end64[32:], 2)
	binary.LittleEndian.PutUint64(end64[40:], uint64(zip64Offset-start))
	binary.LittleEndian.PutUint64(end64[48:], uint64(start))
	out = append(out, end64...)
	locator := make([]byte, 20)
	binary.LittleEndian.PutUint32(locator, 0x07064b50)
	binary.LittleEndian.PutUint64(locator[8:], uint64(zip64Offset))
	binary.LittleEndian.PutUint32(locator[16:], 1)
	out = append(out, locator...)
	eocd := append([]byte{}, data[end:]...)
	binary.LittleEndian.PutUint16(eocd[10:], 0xffff)
	binary.LittleEndian.PutUint32(eocd[12:], 0xffffffff)
	binary.LittleEndian.PutUint32(eocd[16:], 0xffffffff)
	out = append(out, eocd...)

	return out
}
