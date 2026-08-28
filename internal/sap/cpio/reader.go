// Package cpio reads the old portable ASCII CPIO format used by Apple's
// software-update payload. It streams entry bodies so unrelated files are not
// retained while locating the small set needed by the SAP emulator.
package cpio

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
)

const (
	headerSize     = 76
	nameSizeOffset = 59
	fileSizeOffset = 65
)

var (
	headerMagic = []byte("070707")
	trailerName = []byte("TRAILER!!!")
)

type Reader struct {
	source  io.Reader
	current *io.LimitedReader
}

func NewReader(source io.Reader) *Reader {
	return &Reader{source: source}
}

func (r *Reader) Next() (string, io.Reader, error) {
	if r.current != nil {
		if _, err := io.Copy(io.Discard, r.current); err != nil {
			return "", nil, fmt.Errorf("skip CPIO entry: %w", err)
		}
	}

	header := make([]byte, headerSize)
	if _, err := io.ReadFull(r.source, header); err != nil {
		return "", nil, fmt.Errorf("read CPIO header: %w", err)
	}

	if !bytes.Equal(header[:len(headerMagic)], headerMagic) {
		return "", nil, fmt.Errorf("invalid CPIO magic %q", header[:len(headerMagic)])
	}

	nameSize, err := parseOctal(header[nameSizeOffset:fileSizeOffset])
	if err != nil || nameSize < 1 {
		return "", nil, fmt.Errorf("invalid CPIO name size %q", header[nameSizeOffset:fileSizeOffset])
	}

	fileSize, err := parseOctal(header[fileSizeOffset:headerSize])
	if err != nil {
		return "", nil, fmt.Errorf("invalid CPIO file size %q", header[fileSizeOffset:headerSize])
	}

	name := make([]byte, nameSize)
	if _, err := io.ReadFull(r.source, name); err != nil {
		return "", nil, fmt.Errorf("read CPIO name: %w", err)
	}

	if name[len(name)-1] != 0 {
		return "", nil, errors.New("CPIO name is not NUL-terminated")
	}

	name = name[:len(name)-1]
	if bytes.Equal(name, trailerName) {
		return "", nil, io.EOF
	}

	r.current = &io.LimitedReader{R: r.source, N: int64(fileSize)}

	return string(name), r.current, nil
}

func parseOctal(value []byte) (uint64, error) {
	parsed, err := strconv.ParseUint(string(value), 8, 64)
	if err != nil {
		return 0, fmt.Errorf("parse CPIO octal value: %w", err)
	}

	return parsed, nil
}
