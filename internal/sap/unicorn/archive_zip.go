package unicorn

import (
	"archive/zip"
	"errors"
	"fmt"
)

func openZIPEntry(path, name string) (archiveEntry, error) {
	archive, err := zip.OpenReader(path)
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
			size:   candidate.UncompressedSize64,
			close: func() error {
				return errors.Join(reader.Close(), archive.Close())
			},
		}, nil
	}

	_ = archive.Close()

	return archiveEntry{}, fmt.Errorf("unicorn archive does not contain %q", name)
}
