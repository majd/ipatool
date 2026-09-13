package appstore

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AppStore (ZIP streaming framing)", func() {
	DescribeTable("preserves entry data and metadata with streaming-compatible framing",
		func(name string, method, sourceFlags uint16) {
			payload := []byte("streaming archive test payload")
			if strings.HasSuffix(name, "/") {
				payload = []byte{}
			}
			raw := payload
			if method == zip.Deflate {
				var compressed bytes.Buffer
				compressor, err := flate.NewWriter(&compressed, flate.HuffmanOnly)
				Expect(err).ToNot(HaveOccurred())
				_, err = compressor.Write(payload)
				Expect(err).ToNot(HaveOccurred())
				Expect(compressor.Close()).To(Succeed())
				raw = compressed.Bytes()
			}

			By("creating an archive with the requested source framing")
			var source bytes.Buffer
			sourceWriter := zip.NewWriter(&source)
			writer, err := sourceWriter.CreateRaw(&zip.FileHeader{
				Name:               name,
				Method:             method,
				Flags:              sourceFlags,
				CRC32:              crc32.ChecksumIEEE(payload),
				CompressedSize64:   uint64(len(raw)),
				UncompressedSize64: uint64(len(payload)),
				ModifiedDate:       0x5821,
				ModifiedTime:       0x1234,
				ExternalAttrs:      0x81a40000,
			})
			Expect(err).ToNot(HaveOccurred())
			_, err = writer.Write(raw)
			Expect(err).ToNot(HaveOccurred())
			Expect(sourceWriter.Close()).To(Succeed())

			directory, err := os.MkdirTemp("", "ipatool-zip-framing-*")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(os.RemoveAll, directory)
			path := filepath.Join(directory, "source.zip")
			Expect(os.WriteFile(path, source.Bytes(), 0600)).To(Succeed())
			src, err := zip.OpenReader(path)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(src.Close)

			var output bytes.Buffer
			dst := zip.NewWriter(&output)
			Expect((&appstore{}).replicateZip(src, dst, path)).To(Succeed())
			Expect(dst.Close()).To(Succeed())
			result, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
			Expect(err).ToNot(HaveOccurred())
			Expect(result.File).To(HaveLen(1))
			file := result.File[0]
			original := src.File[0]

			By("checking local framing independently of the central directory")
			offset, err := file.DataOffset()
			Expect(err).ToNot(HaveOccurred())
			local := output.Bytes()[:offset]
			flags := binary.LittleEndian.Uint16(local[6:8])
			Expect(flags).To(Equal(file.Flags))
			end := offset + int64(file.CompressedSize64)
			if method == zip.Deflate {
				Expect(flags & 8).To(Equal(uint16(8)))
				Expect(local[14:26]).To(Equal(make([]byte, 12)))
				descriptor := output.Bytes()[end : end+16]
				Expect(binary.LittleEndian.Uint32(descriptor)).To(Equal(uint32(0x08074b50)))
				Expect(binary.LittleEndian.Uint32(descriptor[4:])).To(Equal(file.CRC32))
				Expect(binary.LittleEndian.Uint32(descriptor[8:])).To(Equal(uint32(file.CompressedSize64)))
				Expect(binary.LittleEndian.Uint32(descriptor[12:])).To(Equal(uint32(file.UncompressedSize64)))
			} else {
				Expect(flags & 8).To(BeZero())
				Expect(binary.LittleEndian.Uint32(local[14:])).To(Equal(file.CRC32))
				Expect(binary.LittleEndian.Uint32(local[18:])).To(Equal(uint32(file.CompressedSize64)))
				Expect(binary.LittleEndian.Uint32(local[22:])).To(Equal(uint32(file.UncompressedSize64)))
				Expect(binary.LittleEndian.Uint32(output.Bytes()[end:])).To(Equal(uint32(0x02014b50)))
			}

			By("preserving the compressed bytes and metadata")
			reader, err := file.OpenRaw()
			Expect(err).ToNot(HaveOccurred())
			copied, err := io.ReadAll(reader)
			Expect(err).ToNot(HaveOccurred())
			Expect(copied).To(Equal(raw))
			Expect(file.Name).To(Equal(original.Name))
			Expect(file.Method).To(Equal(original.Method))
			Expect(file.ModifiedDate).To(Equal(original.ModifiedDate))
			Expect(file.ModifiedTime).To(Equal(original.ModifiedTime))
			Expect(file.ExternalAttrs).To(Equal(original.ExternalAttrs))
			Expect(file.Extra).To(Equal(original.Extra))

			By("reading the content back with CRC verification")
			contentReader, err := file.Open()
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(contentReader.Close)
			content, err := io.ReadAll(contentReader)
			Expect(err).ToNot(HaveOccurred())
			Expect(content).To(Equal(payload))
		},
		Entry("directory", "Payload/", zip.Store, uint16(0)),
		Entry("stored file without a descriptor", "stored", zip.Store, uint16(0)),
		Entry("stored file with an existing descriptor", "stored-descriptor", zip.Store, uint16(8)),
		Entry("deflated file without a descriptor", "deflated", zip.Deflate, uint16(0)),
		Entry("deflated file with an existing descriptor", "deflated-descriptor", zip.Deflate, uint16(8)),
	)
})
