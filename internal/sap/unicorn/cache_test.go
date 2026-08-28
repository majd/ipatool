//go:build darwin || linux

package unicorn

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func TestEnsureLibraryDownloadsVerifiesAndCaches(t *testing.T) {
	const member = "unicorn/lib/libunicorn.so.2"

	payload := []byte("test Unicorn library")
	archive := testZip(t, member, payload)

	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		_, _ = response.Write(archive)
	}))
	defer server.Close()

	selected := artifact{
		url:           server.URL,
		archiveSHA256: testSHA256(archive),
		librarySHA256: testSHA256(payload),
		member:        member,
		filename:      "libunicorn.so.2",
	}
	root := t.TempDir()

	path, err := ensureLibrary(context.Background(), root, selected, server.Client())
	if err != nil {
		t.Fatal(err)
	}

	assertFileContents(t, path, payload)

	cachedPath, err := ensureLibrary(context.Background(), root, selected, server.Client())
	if err != nil {
		t.Fatal(err)
	}

	if cachedPath != path {
		t.Fatalf("cached path = %q, want %q", cachedPath, path)
	}

	if count := requests.Load(); count != 1 {
		t.Fatalf("download requests = %d, want 1", count)
	}

	if err := os.WriteFile(path, []byte("corrupted"), 0o600); err != nil {
		t.Fatal(err)
	}

	repairedPath, err := ensureLibrary(context.Background(), root, selected, server.Client())
	if err != nil {
		t.Fatal(err)
	}

	assertFileContents(t, repairedPath, payload)

	if count := requests.Load(); count != 2 {
		t.Fatalf("download requests after repair = %d, want 2", count)
	}
}

func TestEnsureLibraryRejectsArchiveChecksumMismatch(t *testing.T) {
	const member = "unicorn/lib/libunicorn.so.2"

	payload := []byte("test Unicorn library")
	archive := testZip(t, member, payload)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(archive)
	}))
	defer server.Close()

	selected := artifact{
		url:           server.URL,
		archiveSHA256: testSHA256([]byte("different archive")),
		librarySHA256: testSHA256(payload),
		member:        member,
		filename:      "libunicorn.so.2",
	}
	if _, err := ensureLibrary(context.Background(), t.TempDir(), selected, server.Client()); err == nil {
		t.Fatal("expected checksum error")
	}
}

func TestEnsureLibraryRejectsLibraryChecksumMismatch(t *testing.T) {
	const member = "unicorn/lib/libunicorn.so.2"

	payload := []byte("test Unicorn library")
	archive := testZip(t, member, payload)

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(archive)
	}))
	defer server.Close()

	selected := artifact{
		url:           server.URL,
		archiveSHA256: testSHA256(archive),
		librarySHA256: testSHA256([]byte("different library")),
		member:        member,
		filename:      "libunicorn.so.2",
	}
	if _, err := ensureLibrary(context.Background(), t.TempDir(), selected, server.Client()); err == nil {
		t.Fatal("expected checksum error")
	}
}

func testZip(t *testing.T, name string, contents []byte) []byte {
	t.Helper()

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)

	entry, err := archive.Create(name)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := entry.Write(contents); err != nil {
		t.Fatal(err)
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	return buffer.Bytes()
}

func testSHA256(contents []byte) string {
	sum := sha256.Sum256(contents)

	return hex.EncodeToString(sum[:])
}

func assertFileContents(t *testing.T, path string, expected []byte) {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	actual, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(actual, expected) {
		t.Fatalf("contents = %q, want %q", actual, expected)
	}
}
