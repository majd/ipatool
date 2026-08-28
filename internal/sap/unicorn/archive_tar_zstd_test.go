package unicorn

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestOpenTarZstdEntry(t *testing.T) {
	const name = "clangarm64/bin/libunicorn.dll"

	payload := []byte("test Unicorn library")

	var compressed bytes.Buffer

	encoder, err := zstd.NewWriter(&compressed)
	if err != nil {
		t.Fatal(err)
	}

	archive := tar.NewWriter(encoder)
	if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}

	if _, err := archive.Write(payload); err != nil {
		t.Fatal(err)
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "runtime.pkg.tar.zst")
	if err := os.WriteFile(path, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	entry, err := openTarZstdEntry(path, name)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := entry.close(); err != nil {
			t.Errorf("close archive entry: %v", err)
		}
	})

	actual, err := io.ReadAll(entry.reader)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(actual, payload) {
		t.Fatalf("contents = %q, want %q", actual, payload)
	}

	if entry.size != uint64(len(payload)) {
		t.Fatalf("size = %d, want %d", entry.size, len(payload))
	}
}
