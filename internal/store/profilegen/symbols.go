package profilegen

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/blacktop/go-macho"
)

// symbol resolves private value witnesses as well as exported entry points.
// The shared string table is read a page at a time instead of copying it for
// each image in the dependency closure.
func (s *source) symbol(address uint64) string {
	if name := s.symbols[address]; name != "" {
		return name
	}

	for _, name := range s.imageNames() {
		f := s.images[name]
		found := false

		for _, sec := range f.Sections {
			if address >= sec.Addr && address-sec.Addr < sec.Size {
				found = true

				break
			}
		}

		if !found {
			continue
		}

		if !s.symbolsLoaded[name] {
			s.loadSymbols(f)
			s.symbolsLoaded[name] = true
		}

		return s.symbols[address]
	}

	return ""
}
func (s *source) loadSymbols(f *macho.File) {
	header := s.read(f.GetBaseAddress(), 32)
	commands := s.read(f.GetBaseAddress()+32, uint64(binary.LittleEndian.Uint32(header[20:])))

	link := f.Segment("__LINKEDIT")
	if link == nil {
		return
	}

	for off := 0; off+8 <= len(commands); {
		size := int(binary.LittleEndian.Uint32(commands[off+4:]))
		if size < 8 || size > len(commands)-off {
			panic("invalid symbol load command")
		}

		b := commands[off : off+size]
		off += size

		if binary.LittleEndian.Uint32(b) != 2 {
			continue
		}

		if size != 24 {
			panic("invalid symbol table command")
		}

		symoff, nsyms, stroff, strsize := binary.LittleEndian.Uint32(b[8:]), binary.LittleEndian.Uint32(b[12:]), binary.LittleEndian.Uint32(b[16:]), binary.LittleEndian.Uint32(b[20:])
		if uint64(symoff) < link.Offset || uint64(stroff) < link.Offset || nsyms > 4<<20 {
			panic("invalid cache symbol table")
		}

		symbols := s.read(link.Addr+uint64(symoff)-link.Offset, uint64(nsyms)*16)
		for i := uint32(0); i < nsyms; i++ {
			entry := symbols[i*16:]
			a := binary.LittleEndian.Uint64(entry[8:])
			idx := binary.LittleEndian.Uint32(entry)

			if a == 0 || entry[4]&0xe0 != 0 || entry[4]&0xe != 0xe || idx >= strsize || s.symbols[a] != "" {
				continue
			}

			text := s.cstring(link.Addr + uint64(stroff) - link.Offset + uint64(idx))
			if len(text) > 1 {
				s.symbols[a] = strings.TrimPrefix(text, "_")
			}
		}

		return
	}

	panic(fmt.Sprintf("no symbol table in %s", f.UUID()))
}
