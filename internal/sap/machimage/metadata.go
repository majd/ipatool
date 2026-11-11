package machimage

import "github.com/blacktop/go-macho"

// Symbols returns the immutable symbol table used for guest exports and ivars.
// Callers must not modify the returned slice.
func (i *Image) Symbols() []macho.Symbol {
	if i.file.Symtab == nil {
		return nil
	}

	return i.file.Symtab.Syms
}

// InstructionRanges returns the guest's executable section address ranges.
func (i *Image) InstructionRanges() [][2]uint64 {
	var ranges [][2]uint64

	for _, section := range i.file.Sections {
		if section.Flags.IsPureInstructions() || section.Flags.IsSomeInstructions() {
			ranges = append(ranges, [2]uint64{section.Addr, section.Addr + section.Size})
		}
	}

	return ranges
}
