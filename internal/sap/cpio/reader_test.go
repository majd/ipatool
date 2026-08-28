package cpio

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

func TestReaderStreamsEntries(t *testing.T) {
	archive := append(cpioEntry("first", []byte("ignored")), cpioEntry("second", []byte("wanted"))...)
	archive = append(archive, cpioEntry("TRAILER!!!", nil)...)
	reader := NewReader(bytes.NewReader(archive))

	name, _, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}

	if name != "first" {
		t.Fatalf("name = %q, want first", name)
	}

	name, body, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}

	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}

	if name != "second" || string(data) != "wanted" {
		t.Fatalf("entry = %q %q, want second wanted", name, data)
	}

	if _, _, err := reader.Next(); err != io.EOF {
		t.Fatalf("trailer error = %v, want EOF", err)
	}
}

func cpioEntry(name string, body []byte) []byte {
	nameBytes := append([]byte(name), 0)
	header := fmt.Sprintf(
		"070707%06o%06o%06o%06o%06o%06o%06o%011o%06o%011o",
		0, 0, 0, 0, 0, 1, 0, 0, len(nameBytes), len(body),
	)
	entry := append([]byte(header), nameBytes...)

	return append(entry, body...)
}
