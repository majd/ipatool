package assets

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"net/http"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("macOS 27 remote framework extraction", Label("integration"), func() {
	It("reads the pinned framework files without an extraction executable", func() {
		if os.Getenv("IPATOOL_INTEGRATION_TESTS") == "" {
			Skip("set IPATOOL_INTEGRATION_TESTS to run")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		DeferCleanup(cancel)

		client := &http.Client{}
		remote, err := openRemoteFile(ctx, client, macOSRestoreURL)
		Expect(err).NotTo(HaveOccurred())

		archive, err := zip.NewReader(remote, remote.size)
		Expect(err).NotTo(HaveOccurred())
		entry, err := pinnedZipEntry(archive, fileSystemImageName, fileSystemArchiveSize, fileSystemArchiveSize)
		Expect(err).NotTo(HaveOccurred())
		destination := GinkgoT().TempDir()
		Expect(extractFrameworkFiles(ctx, client, remote, entry, destination)).To(Succeed())

		for _, spec := range requiredFiles[1:] {
			data, err := os.ReadFile(filepath.Join(destination, spec.name))
			Expect(err).NotTo(HaveOccurred(), spec.name)
			Expect(data).To(HaveLen(spec.size), spec.name)
			Expect(sha256.Sum256(data)).To(Equal(spec.digest), spec.name)
		}
	})

	It("reads AppleMediaServices from the pinned arm64e shared cache", func() {
		if os.Getenv("IPATOOL_INTEGRATION_TESTS") == "" {
			Skip("set IPATOOL_INTEGRATION_TESTS to run")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		DeferCleanup(cancel)

		client := &http.Client{}
		remote, err := openRemoteFile(ctx, client, macOSRestoreURL)
		Expect(err).NotTo(HaveOccurred())
		archive, err := zip.NewReader(remote, remote.size)
		Expect(err).NotTo(HaveOccurred())
		entry, err := pinnedZipEntry(archive, systemOSImageName, systemOSArchiveSize, systemOSArchiveSize)
		Expect(err).NotTo(HaveOccurred())

		workspace := GinkgoT().TempDir()
		output := filepath.Join(workspace, "AppleMediaServices")
		Expect(extractAppleMediaServices(ctx, client, remote, entry, output)).To(Succeed())
		data, err := os.ReadFile(output)
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(HaveLen(requiredFiles[0].size))
		Expect(sha256.Sum256(data)).To(Equal(requiredFiles[0].digest))
	})
})
