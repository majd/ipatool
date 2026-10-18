package assets

import (
	"bytes"
	"encoding/binary"
	"io"

	machotypes "github.com/blacktop/go-macho/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type sparseSymbols struct {
	data    []byte
	largest int
}

// ReadAt simulates a large sparse cache and records the largest requested allocation.
func (s *sparseSymbols) ReadAt(output []byte, offset int64) (int, error) {
	s.largest = max(s.largest, len(output))

	if offset < 0 {
		return 0, io.EOF
	}

	clear(output)

	if offset < int64(len(s.data)) {
		copy(output, s.data[offset:])
	}

	return len(output), nil
}

var _ = Describe("Shared-cache symbol loading", func() {
	It("reads only referenced strings from a gigabyte-sized string table", func() {
		raw := &sparseSymbols{data: make([]byte, 2048)}
		names := []byte("\x00_regular\x00_Go.symbol\x00_target.symbol\x00")
		copy(raw.data[1024:], names)
		entries := []machotypes.Nlist64{{Nlist: machotypes.Nlist{Name: 1, Type: 0x0f}, Value: 0x180001000}, {Nlist: machotypes.Nlist{Name: 10, Type: 0x0b}, Value: 21}}
		var table bytes.Buffer
		for _, entry := range entries {
			Expect(binary.Write(&table, binary.LittleEndian, entry)).To(Succeed())
		}
		copy(raw.data[128:], table.Bytes())
		var command bytes.Buffer
		header := machotypes.SymtabCmd{LoadCmd: machotypes.LC_SYMTAB, Len: 24, Symoff: 128, Nsyms: 2, Stroff: 1024, Strsize: 1 << 30}
		Expect(binary.Write(&command, binary.LittleEndian, header)).To(Succeed())
		result, err := readImageSymbols(&dyldCacheMember{file: raw, size: 2 << 30}, command.Bytes())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Syms).To(HaveLen(2))
		Expect(result.Syms[0].Name).To(Equal("_regular"))
		Expect(result.Syms[1].Name).To(Equal("Go.symbol"))
		Expect(result.Syms[1].IndirectName).To(Equal("target.symbol"))
		Expect(raw.largest).To(BeNumerically("<=", 4096))
	})
	It("rejects symbol data extending beyond the cache member", func() {
		var command bytes.Buffer
		Expect(binary.Write(&command, binary.LittleEndian, machotypes.SymtabCmd{LoadCmd: machotypes.LC_SYMTAB, Len: 24, Symoff: 128, Nsyms: 10, Stroff: 256, Strsize: 128})).To(Succeed())
		_, err := readImageSymbols(&dyldCacheMember{file: bytes.NewReader(make([]byte, 256)), size: 256}, command.Bytes())
		Expect(err).To(MatchError(ContainSubstring("invalid image symbol table")))
	})
})
