package appstore

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha1" // #nosec G505 -- XAR uses SHA-1 for archive checksums.
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	apphttp "github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AppStore (macOS download)", func() {
	var (
		server      *httptest.Server
		destination string
		encrypted   []byte
		decrypted   []byte
		dpInfo      []byte
		hardwareID  []byte
		decrypter   *fakeMacPackageDecrypter
		factory     macPackageDecrypterFactory
	)

	BeforeEach(func() {
		encrypted = []byte("encrypted package")
		decrypted = makeTestXAR([]byte("decrypted payload"), false)
		dpInfo = []byte("dp info")
		hardwareID = []byte("hardware")

		server = httptest.NewServer(gohttp.HandlerFunc(func(writer gohttp.ResponseWriter, _ *gohttp.Request) {
			_, _ = writer.Write(encrypted)
		}))
		DeferCleanup(server.Close)

		destination = filepath.Join(GinkgoT().TempDir(), "app.pkg")
		decrypter = &fakeMacPackageDecrypter{output: decrypted}
		factory = func(ctx context.Context, gotHardwareID, gotDPInfo []byte) (macPackageDecrypter, error) {
			Expect(ctx).ToNot(BeNil())
			Expect(gotHardwareID).To(Equal(hardwareID))
			Expect(gotDPInfo).To(Equal(dpInfo))

			return decrypter, nil
		}
	})

	download := func(operatingSystem operatingsystem.OperatingSystem) (DownloadOutput, error) {
		store := &appstore{
			httpClient:          apphttp.NewClient[interface{}](apphttp.Args{}),
			macDecrypterFactory: factory,
			os:                  operatingSystem,
		}

		return store.downloadMacPackage(context.Background(), downloadItemResult{
			URL:   server.URL,
			Sinfs: []Sinf{{DPInfo: dpInfo}},
		}, destination, hardwareID, nil)
	}

	expectNoStaging := func() {
		Expect(destination + macEncryptedStageSuffix).ToNot(BeAnExistingFile())
		Expect(destination + macDecryptedStageSuffix).ToNot(BeAnExistingFile())
		Expect(destination + macBackupSuffix).ToNot(BeAnExistingFile())
	}

	It("publishes only the decrypted validated package", func() {
		out, err := download(operatingsystem.New())

		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(Equal(DownloadOutput{DestinationPath: destination}))
		Expect(os.ReadFile(destination)).To(Equal(decrypted))
		Expect(decrypter.input).To(Equal(encrypted))
		Expect(decrypter.closed).To(BeTrue())
		Expect(destination + macDPInfoSuffix).ToNot(BeAnExistingFile())
		Expect(destination + macHWInfoSuffix).ToNot(BeAnExistingFile())
		expectNoStaging()
	})

	It("replaces an existing package and removes legacy sidecars after success", func() {
		Expect(os.WriteFile(destination, []byte("old package"), 0644)).To(Succeed())
		Expect(os.WriteFile(destination+macDPInfoSuffix, []byte("old dp"), 0644)).To(Succeed())
		Expect(os.WriteFile(destination+macHWInfoSuffix, []byte("old hw"), 0644)).To(Succeed())

		_, err := download(operatingsystem.New())

		Expect(err).ToNot(HaveOccurred())
		Expect(os.ReadFile(destination)).To(Equal(decrypted))
		Expect(destination + macDPInfoSuffix).ToNot(BeAnExistingFile())
		Expect(destination + macHWInfoSuffix).ToNot(BeAnExistingFile())
		expectNoStaging()
	})

	It("succeeds when legacy sidecar cleanup fails after publication", func() {
		Expect(os.WriteFile(destination+macDPInfoSuffix, []byte("old dp"), 0644)).To(Succeed())
		operatingSystem := &macDownloadFailureOS{
			OperatingSystem: operatingsystem.New(),
			removeErrorPath: destination + macDPInfoSuffix,
		}

		out, err := download(operatingSystem)

		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(Equal(DownloadOutput{DestinationPath: destination}))
		Expect(os.ReadFile(destination)).To(Equal(decrypted))
		Expect(os.ReadFile(destination + macDPInfoSuffix)).To(Equal([]byte("old dp")))
		expectNoStaging()
	})

	It("preserves the existing package when decryption fails", func() {
		Expect(os.WriteFile(destination, []byte("old package"), 0644)).To(Succeed())
		decrypter.err = errors.New("decrypt failed")

		_, err := download(operatingsystem.New())

		Expect(err).To(MatchError(ContainSubstring("decrypt failed")))
		Expect(os.ReadFile(destination)).To(Equal([]byte("old package")))
		expectNoStaging()
	})

	It("cancels the package request and cleans staging", func() {
		started := make(chan struct{})
		cancelServer := httptest.NewServer(gohttp.HandlerFunc(func(writer gohttp.ResponseWriter, request *gohttp.Request) {
			close(started)
			<-request.Context().Done()
		}))
		DeferCleanup(cancelServer.Close)

		ctx, cancel := context.WithCancel(context.Background())
		store := &appstore{
			httpClient:          apphttp.NewClient[interface{}](apphttp.Args{}),
			macDecrypterFactory: factory,
			os:                  operatingsystem.New(),
		}
		done := make(chan error, 1)
		go func() {
			_, err := store.downloadMacPackage(ctx, downloadItemResult{
				URL:   cancelServer.URL,
				Sinfs: []Sinf{{DPInfo: dpInfo}},
			}, destination, hardwareID, nil)
			done <- err
		}()

		<-started
		cancel()

		Eventually(done).Should(Receive(MatchError(ContainSubstring(context.Canceled.Error()))))
		Expect(destination).ToNot(BeAnExistingFile())
		expectNoStaging()
	})

	It("preserves the existing package when decrypted output is not XAR", func() {
		Expect(os.WriteFile(destination, []byte("old package"), 0644)).To(Succeed())
		decrypter.output = []byte("not xar")

		_, err := download(operatingsystem.New())

		Expect(err).To(MatchError(ContainSubstring("failed to parse XAR archive")))
		Expect(os.ReadFile(destination)).To(Equal([]byte("old package")))
		expectNoStaging()
	})

	It("rejects a XAR without files", func() {
		Expect(os.WriteFile(destination, []byte("old package"), 0644)).To(Succeed())
		decrypter.output = makeEmptyTestXAR()

		_, err := download(operatingsystem.New())

		Expect(err).To(MatchError(ContainSubstring("does not contain files")))
		Expect(os.ReadFile(destination)).To(Equal([]byte("old package")))
		expectNoStaging()
	})

	It("rejects a XAR entry with an invalid checksum", func() {
		Expect(os.WriteFile(destination, []byte("old package"), 0644)).To(Succeed())
		decrypter.output = makeTestXAR([]byte("decrypted payload"), true)

		_, err := download(operatingsystem.New())

		Expect(err).To(MatchError(ContainSubstring("failed checksum validation")))
		Expect(os.ReadFile(destination)).To(Equal([]byte("old package")))
		expectNoStaging()
	})

	It("restores the existing package when publication fails", func() {
		Expect(os.WriteFile(destination, []byte("old package"), 0644)).To(Succeed())
		operatingSystem := &macDownloadFailureOS{
			OperatingSystem: operatingsystem.New(),
			renameErrorOld:  destination + macDecryptedStageSuffix,
		}

		_, err := download(operatingSystem)

		Expect(err).To(MatchError(ContainSubstring("failed to publish package")))
		Expect(os.ReadFile(destination)).To(Equal([]byte("old package")))
		expectNoStaging()
	})

	It("does not replace an unrelated existing backup", func() {
		Expect(os.WriteFile(destination, []byte("old package"), 0644)).To(Succeed())
		backupPath := destination + macBackupSuffix
		Expect(os.WriteFile(backupPath, []byte("unrelated backup"), 0644)).To(Succeed())

		_, err := download(operatingsystem.New())

		Expect(err).ToNot(HaveOccurred())
		Expect(os.ReadFile(destination)).To(Equal(decrypted))
		Expect(os.ReadFile(backupPath)).To(Equal([]byte("unrelated backup")))
		Expect(destination + macBackupSuffix + ".1").ToNot(BeAnExistingFile())
	})

	It("rejects a download response without dpInfo before downloading", func() {
		factoryCalled := false
		factory = func(context.Context, []byte, []byte) (macPackageDecrypter, error) {
			factoryCalled = true

			return decrypter, nil
		}
		store := &appstore{
			httpClient:          apphttp.NewClient[interface{}](apphttp.Args{}),
			macDecrypterFactory: factory,
			os:                  operatingsystem.New(),
		}

		_, err := store.downloadMacPackage(context.Background(), downloadItemResult{URL: server.URL}, destination, hardwareID, nil)

		Expect(err).To(MatchError(ContainSubstring("dpInfo")))
		Expect(factoryCalled).To(BeFalse())
		Expect(destination).ToNot(BeAnExistingFile())
	})

	It("rejects conflicting dpInfo values", func() {
		_, err := macDPInfo([]Sinf{{DPInfo: []byte("one")}, {DPInfo: []byte("two")}})
		Expect(err).To(MatchError(ContainSubstring("conflicting")))
	})
})

type fakeMacPackageDecrypter struct {
	output []byte
	input  []byte
	err    error
	closed bool
}

//nolint:wrapcheck
func (f *fakeMacPackageDecrypter) Decrypt(_ context.Context, dst io.Writer, src io.Reader) (int64, error) {
	input, err := io.ReadAll(src)
	if err != nil {
		return 0, err
	}

	f.input = input
	if f.err != nil {
		return 0, f.err
	}

	n, err := dst.Write(f.output)

	return int64(n), err
}

func (f *fakeMacPackageDecrypter) Close() error {
	f.closed = true

	return nil
}

type macDownloadFailureOS struct {
	operatingsystem.OperatingSystem
	renameErrorOld  string
	removeErrorPath string
}

//nolint:wrapcheck
func (o *macDownloadFailureOS) Remove(path string) error {
	if path == o.removeErrorPath {
		return errors.New("remove failed")
	}

	return o.OperatingSystem.Remove(path)
}

//nolint:wrapcheck
func (o *macDownloadFailureOS) Rename(oldPath, newPath string) error {
	if oldPath == o.renameErrorOld {
		return errors.New("rename failed")
	}

	return o.OperatingSystem.Rename(oldPath, newPath)
}

func makeEmptyTestXAR() []byte {
	return makeTestXARArchive("")
}

func makeTestXAR(payload []byte, corruptFileChecksum bool) []byte {
	payloadChecksum := sha1.Sum(payload) // #nosec G401 -- XAR uses SHA-1 for archive checksums.
	checksum := payloadChecksum[:]

	if corruptFileChecksum {
		checksum = bytes.Repeat([]byte{0xff}, sha1.Size)
	}

	const tocChecksumSize = sha1.Size
	fileXML := fmt.Sprintf(`<file id="1"><type>file</type><name>Payload</name><data><length>%d</length><offset>%d</offset><size>%d</size><encoding style="application/octet-stream"/><archived-checksum style="sha1">%s</archived-checksum><extracted-checksum style="sha1">%s</extracted-checksum></data></file>`,
		len(payload),
		tocChecksumSize,
		len(payload),
		hex.EncodeToString(checksum),
		hex.EncodeToString(payloadChecksum[:]),
	)

	return makeTestXARArchive(fileXML, payload...)
}

func makeTestXARArchive(fileXML string, payload ...byte) []byte {
	const tocChecksumSize = sha1.Size
	toc := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<xar><toc><checksum style="sha1"><offset>0</offset><size>%d</size></checksum>%s</toc></xar>`, tocChecksumSize, fileXML)

	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, err := writer.Write([]byte(toc))
	Expect(err).ToNot(HaveOccurred())
	Expect(writer.Close()).To(Succeed())

	compressedTOC := compressed.Bytes()
	tocChecksum := sha1.Sum(compressedTOC) // #nosec G401 -- XAR uses SHA-1 for archive checksums.

	archive := make([]byte, 28+len(compressedTOC)+tocChecksumSize+len(payload))
	binary.BigEndian.PutUint32(archive[0:4], 0x78617221)
	binary.BigEndian.PutUint16(archive[4:6], 28)
	binary.BigEndian.PutUint16(archive[6:8], 1)
	binary.BigEndian.PutUint64(archive[8:16], uint64(len(compressedTOC)))
	binary.BigEndian.PutUint64(archive[16:24], uint64(len(toc)))
	binary.BigEndian.PutUint32(archive[24:28], 1)
	copy(archive[28:], compressedTOC)
	copy(archive[28+len(compressedTOC):], tocChecksum[:])
	copy(archive[28+len(compressedTOC)+tocChecksumSize:], payload)

	return archive
}
