package assets

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/blacktop/go-macho"
	machotypes "github.com/blacktop/go-macho/types"
)

// imageSymbols restores symbol load commands without allocating the cache-wide string table.
// Export needs only this image's nlists and the strings they actually reference.
func (c *dyldCache) imageSymbols(image *dyldImage, f *macho.File) error {
	header := make([]byte, dyldMachHeader64Size)
	if _, err := image.member.file.ReadAt(header, int64(image.offset)); err != nil {
		return fmt.Errorf("read image header: %w", err)
	}

	size := uint64(binary.LittleEndian.Uint32(header[20:]))
	if size > dyldMaximumLoadCommandsSize {
		return fmt.Errorf("invalid image load command size")
	}

	commands := make([]byte, size)
	if _, err := image.member.file.ReadAt(commands, int64(image.offset)+dyldMachHeader64Size); err != nil {
		return fmt.Errorf("read image commands: %w", err)
	}

	address, err := image.linkeditAddress()
	if err != nil {
		return err
	}

	mapping, err := c.mappingForAddress(address)
	if err != nil {
		return err
	}

	loads := make([]macho.Load, 0, len(f.Loads)+2)
	index := 0

	for offset := 0; offset < len(commands); {
		if len(commands)-offset < 8 {
			return fmt.Errorf("truncated image command")
		}

		length := int(binary.LittleEndian.Uint32(commands[offset+4:]))
		if length < 8 || length > len(commands)-offset {
			return fmt.Errorf("invalid image command size")
		}

		raw := commands[offset : offset+length]
		offset += length

		command := machotypes.LoadCmd(binary.LittleEndian.Uint32(raw))
		switch command {
		case machotypes.LC_SYMTAB:
			table, err := readImageSymbols(mapping.member, raw)
			if err != nil {
				return err
			}

			f.Symtab = table
			loads = append(loads, table)
		case machotypes.LC_DYSYMTAB:
			table := &macho.Dysymtab{LoadBytes: raw}
			if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &table.DysymtabCmd); err != nil {
				return fmt.Errorf("decode dynamic symbols: %w", err)
			}

			if f.Symtab == nil || uint64(table.Iundefsym)+uint64(table.Nundefsym) > uint64(len(f.Symtab.Syms)) || table.Nindirectsyms > 4<<20 {
				return fmt.Errorf("invalid dynamic symbol table")
			}

			if !validDyldRange(mapping.member.size, uint64(table.Indirectsymoff), uint64(table.Nindirectsyms)*4) {
				return fmt.Errorf("indirect symbols exceed cache member")
			}

			encoded := make([]byte, int(table.Nindirectsyms)*4)
			if _, err := mapping.member.file.ReadAt(encoded, int64(table.Indirectsymoff)); err != nil {
				return fmt.Errorf("read indirect symbols: %w", err)
			}

			table.IndirectSyms = make([]uint32, table.Nindirectsyms)
			if err := binary.Read(bytes.NewReader(encoded), binary.LittleEndian, table.IndirectSyms); err != nil {
				return fmt.Errorf("decode indirect symbols: %w", err)
			}

			f.Dysymtab = table
			loads = append(loads, table)
		default:
			if index >= len(f.Loads) || f.Loads[index].Command() != command {
				return fmt.Errorf("image load command order changed")
			}

			loads = append(loads, f.Loads[index])
			index++
		}
	}

	f.Loads = loads

	return nil
}

// readImageSymbols decodes just the referenced names, preserving go-macho's symbol normalization.
func readImageSymbols(member *dyldCacheMember, raw []byte) (*macho.Symtab, error) {
	table := &macho.Symtab{LoadBytes: raw}
	if err := binary.Read(bytes.NewReader(raw), binary.LittleEndian, &table.SymtabCmd); err != nil {
		return nil, fmt.Errorf("decode symbols: %w", err)
	}

	if table.Nsyms > 4<<20 || !validDyldRange(member.size, uint64(table.Symoff), uint64(table.Nsyms)*16) || !validDyldRange(member.size, uint64(table.Stroff), uint64(table.Strsize)) {
		return nil, fmt.Errorf("invalid image symbol table")
	}

	encoded := make([]byte, int(table.Nsyms)*16)
	if _, err := member.file.ReadAt(encoded, int64(table.Symoff)); err != nil {
		return nil, fmt.Errorf("read image symbols: %w", err)
	}

	readName := func(offset uint64) (string, error) {
		if offset >= uint64(table.Strsize) {
			return "", nil
		}

		name, err := readDyldCString(member.file, int64(uint64(table.Stroff)+uint64(table.Strsize)), uint64(table.Stroff)+offset)
		if err != nil {
			return "", err
		}

		if strings.HasPrefix(name, "_") && strings.Contains(name, ".") {
			name = name[1:]
		}

		return name, nil
	}
	reader := bytes.NewReader(encoded)

	table.Syms = make([]macho.Symbol, 0, table.Nsyms)
	for i := uint32(0); i < table.Nsyms; i++ {
		var n machotypes.Nlist64
		if err := binary.Read(reader, binary.LittleEndian, &n); err != nil {
			return nil, fmt.Errorf("decode image symbol: %w", err)
		}

		name, err := readName(uint64(n.Name))
		if err != nil {
			return nil, err
		}

		indirect := ""
		if n.Type.IsIndirectSym() {
			indirect, err = readName(n.Value)
			if err != nil {
				return nil, err
			}
		}

		table.Syms = append(table.Syms, macho.Symbol{Name: name, IndirectName: indirect, Type: n.Type, Sect: n.Sect, Desc: n.Desc, Value: n.Value})
	}

	return table, nil
}
