package unicorn

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

func openTarZstdEntry(path, name string) (archiveEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return archiveEntry{}, fmt.Errorf("open Unicorn archive: %w", err)
	}

	decoder, err := zstd.NewReader(file)
	if err != nil {
		_ = file.Close()

		return archiveEntry{}, fmt.Errorf("open Unicorn archive: %w", err)
	}

	closeArchive := func() error {
		decoder.Close()

		return file.Close()
	}

	reader := tar.NewReader(decoder)

	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			_ = closeArchive()

			return archiveEntry{}, fmt.Errorf("unicorn archive does not contain %q", name)
		}

		if err != nil {
			_ = closeArchive()

			return archiveEntry{}, fmt.Errorf("read Unicorn archive: %w", err)
		}

		if header.Name != name {
			continue
		}

		if header.Typeflag != tar.TypeReg || header.Size < 0 {
			_ = closeArchive()

			return archiveEntry{}, fmt.Errorf("invalid Unicorn library entry %q", name)
		}

		return archiveEntry{
			reader: io.LimitReader(reader, header.Size),
			size:   uint64(header.Size),
			close:  closeArchive,
		}, nil
	}
}
