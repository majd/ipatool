package appstore

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
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

func TestApplyPatchesReportsZipWriteFailure(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "source.zip"), filepath.Join(dir, "patched.ipa")

	file, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}

	writer := zip.NewWriter(file)
	if _, err := writer.Create("Payload/App.app/"); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	// The patched archive is small enough to stay buffered until the zip writer
	// is closed, so the write failure only surfaces when the archive is finalized.
	store := &appstore{os: readOnlyDestinationOS{OperatingSystem: operatingsystem.New(), destination: dst}}

	err = store.applyPatches(downloadItemResult{Metadata: map[string]interface{}{}}, Account{}, src, dst, nil)
	if err == nil {
		t.Fatal("expected applyPatches to report that the patched archive could not be written")
	}
}
