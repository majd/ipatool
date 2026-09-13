package appstore

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type readOnlyDestinationOS struct {
	operatingsystem.OperatingSystem
	destination string
}

func (o readOnlyDestinationOS) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	if name != o.destination {
		file, err := o.OperatingSystem.OpenFile(name, flag, perm)
		if err != nil {
			return nil, fmt.Errorf("failed to open file: %w", err)
		}

		return file, nil
	}

	if err := os.WriteFile(name, nil, perm); err != nil {
		return nil, fmt.Errorf("failed to create destination: %w", err)
	}

	// Hand back a read-only descriptor so every write to the archive fails.
	file, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("failed to open destination: %w", err)
	}

	return file, nil
}

var _ = Describe("AppStore (patch finalization)", func() {
	It("reports buffered writes that fail when closing the ZIP writer", func() {
		dir := GinkgoT().TempDir()
		src, dst := filepath.Join(dir, "source.zip"), filepath.Join(dir, "patched.ipa")

		var source bytes.Buffer
		writer := zip.NewWriter(&source)
		_, err := writer.Create("Payload/App.app/")
		Expect(err).ToNot(HaveOccurred())
		Expect(writer.Close()).To(Succeed())
		Expect(os.WriteFile(src, source.Bytes(), 0600)).To(Succeed())

		// The patched archive stays buffered until Close, where the read-only
		// destination descriptor rejects the write.
		store := &appstore{os: readOnlyDestinationOS{OperatingSystem: operatingsystem.New(), destination: dst}}
		err = store.applyPatches(downloadItemResult{Metadata: map[string]interface{}{}}, Account{}, src, dst, nil)
		Expect(err).To(MatchError(ContainSubstring("failed to close zip writer")))
		var pathErr *os.PathError
		Expect(errors.As(err, &pathErr)).To(BeTrue())
		Expect(pathErr.Op).To(Equal("write"))
		Expect(pathErr.Path).To(Equal(dst))
	})
})
