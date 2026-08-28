package unicorn

import "io"

type archiveFormat uint8

const (
	archiveZIP archiveFormat = iota
	archiveSevenZip
	archiveTarZstd
)

type archiveEntry struct {
	reader io.Reader
	size   uint64
	close  func() error
}
