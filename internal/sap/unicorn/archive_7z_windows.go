//go:build windows

package unicorn

import (
	"errors"
	"fmt"

	"github.com/bodgit/sevenzip"
)

func openArchiveEntry(path, name string, format archiveFormat) (archiveEntry, error) {
	switch format {
	case archiveZIP:
		return openZIPEntry(path, name)
	case archiveSevenZip:
		return openSevenZipEntry(path, name)
	case archiveTarZstd:
		return openTarZstdEntry(path, name)
	default:
		return archiveEntry{}, fmt.Errorf("unsupported Unicorn archive format %d", format)
	}
}

func openSevenZipEntry(path, name string) (archiveEntry, error) {
	archive, err := sevenzip.OpenReader(path)
	if err != nil {
		return archiveEntry{}, fmt.Errorf("open Unicorn archive: %w", err)
	}

	for _, candidate := range archive.File {
		if candidate.Name != name {
			continue
		}
		if candidate.FileInfo().IsDir() {
			_ = archive.Close()
			return archiveEntry{}, fmt.Errorf("invalid Unicorn library entry %q", name)
		}

		reader, err := candidate.Open()
		if err != nil {
			_ = archive.Close()
			return archiveEntry{}, fmt.Errorf("open Unicorn library entry: %w", err)
		}
		return archiveEntry{
			reader: reader,
			size:   candidate.UncompressedSize,
			close: func() error {
				return errors.Join(reader.Close(), archive.Close())
			},
		}, nil
	}

	_ = archive.Close()
	return archiveEntry{}, fmt.Errorf("Unicorn archive does not contain %q", name)
}
