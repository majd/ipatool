package profilegen

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
)

type typeRequest struct{ Address, Length uint64 }
type metadataAccessor struct{ Descriptor, Address uint64 }
type globalRequest struct {
	Name    string
	Address uint64
}
type swiftRequests struct {
	Types        []typeRequest
	Accessors    []string
	Globals      []globalRequest
	Nominals     []metadataAccessor
	Conformances []uint64
}

// mangledNameLength honors the embedded relative and absolute symbolic references
// described by Swift's ABI; their payloads can contain zero bytes.
func mangledNameLength(data []byte) (int, error) {
	for i := 0; i < len(data); {
		b := data[i]
		if b == 0 {
			return i, nil
		}

		i++

		size := 0

		switch {
		case b >= 1 && b <= 0x17:
			size = 4
		case b >= 0x18 && b <= 0x1f:
			size = 8
		}

		if size > len(data)-i {
			return 0, fmt.Errorf("truncated Swift symbolic reference")
		}

		i += size
	}

	return 0, fmt.Errorf("unterminated Swift mangled name")
}

// concreteTypeRecords reads the compiler's relative-name/length records. Matching
// both fields against actual typeref boundaries excludes reflection records and
// unrelated constants; no virtual addresses are supplied by the host.
func concreteTypeRecords(base uint64, data []byte, names map[uint64]uint64) []typeRequest {
	var result []typeRequest

	for off := 0; off+8 <= len(data); off += 4 {
		address := uint64(int64(base+uint64(off)) + int64(int32(binary.LittleEndian.Uint32(data[off:]))))
		length := uint64(binary.LittleEndian.Uint32(data[off+4:]))

		if length != 0 && names[address] == length {
			result = append(result, typeRequest{address, length})
		}
	}

	return result
}

// discoverSwiftRequests inventories the concrete metadata records, nominal type
// descriptors, imported accessors, and protocol conformances of the Store images.
func (s *source) discoverSwiftRequests() (*swiftRequests, error) {
	req := &swiftRequests{}
	types := map[uint64]uint64{}
	conformances := map[uint64]bool{}
	exportedConformances := map[uint64]bool{}

	for name, a := range s.exports {
		if strings.HasSuffix(name, "Mc") && strings.HasPrefix(name, "_$s") {
			exportedConformances[a] = true
		}
	}

	for _, name := range []string{amsImage, authImage} {
		f := s.images[name]
		names := map[uint64]uint64{}

		for _, sec := range f.Sections {
			if sec.Name != "__swift5_typeref" {
				continue
			}

			data := s.read(sec.Addr, sec.Size)
			for off := 0; off < len(data); {
				if data[off] == 0 || data[off] == 0xff {
					off++

					continue
				}

				n, err := mangledNameLength(data[off:])
				if err != nil {
					return nil, fmt.Errorf("read %s type references: %w", name, err)
				}

				names[sec.Addr+uint64(off)] = uint64(n)
				off += n + 1
			}
		}

		for _, sec := range f.Sections {
			if sec.Flags.IsPureInstructions() || sec.Flags.IsSomeInstructions() {
				data := s.read(sec.Addr, sec.Size)
				for off := 0; off+8 <= len(data); off += 4 {
					slot, ok := referencedPointer(binary.LittleEndian.Uint32(data[off:]), binary.LittleEndian.Uint32(data[off+4:]), sec.Addr+uint64(off))
					if ok && s.cache.Contains(slot, 8) {
						if a := s.u64(slot); exportedConformances[a] {
							conformances[a] = true
						}
					}
				}
			}

			switch sec.Name {
			case "__const":
				if sec.Seg != "__TEXT" {
					continue
				}

				for _, r := range concreteTypeRecords(sec.Addr, s.read(sec.Addr, sec.Size), names) {
					types[r.Address] = r.Length
				}
			case "__swift5_types":
				for off := uint64(0); off+4 <= sec.Size; off += 4 {
					d := s.relative(sec.Addr + off)
					flags := s.u32(d)

					if flags&0x80 != 0 || flags&0x1f < 16 || flags&0x1f > 18 || s.u32(d+12) == 0 {
						continue
					}

					req.Nominals = append(req.Nominals, metadataAccessor{d, s.relative(d + 12)})
				}
			case "__swift5_proto":
				for off := uint64(0); off+4 <= sec.Size; off += 4 {
					conformances[s.relative(sec.Addr+off)] = true
				}
			case "__got", "__auth_got":
				for off := uint64(0); off+8 <= sec.Size; off += 8 {
					a := s.u64(sec.Addr + off)
					if exportedConformances[a] {
						conformances[a] = true
					}
				}
			}
		}
	}

	for a, n := range types {
		req.Types = append(req.Types, typeRequest{a, n})
	}

	for a := range conformances {
		req.Conformances = append(req.Conformances, a)
	}

	calls := s.storeCalls()
	accessors, globals := map[string]bool{}, map[string]bool{}

	for _, c := range calls {
		name := c.Symbol
		if !strings.HasPrefix(name, "$s") {
			continue
		}

		if strings.HasSuffix(name, "Ma") {
			d := s.exports["_"+strings.TrimSuffix(name, "Ma")+"Mn"]
			if d != 0 && s.u32(d)&0x80 == 0 {
				accessors[name] = true
			}
		}

		if strings.HasSuffix(name, "vau") {
			globals[name] = true
		}
	}

	for name := range accessors {
		req.Accessors = append(req.Accessors, name)
	}

	for name := range globals {
		a := s.exports["_"+name]
		if a == 0 {
			return nil, fmt.Errorf("missing Swift global accessor %s", name)
		}

		req.Globals = append(req.Globals, globalRequest{name, a})
	}

	sort.Slice(req.Types, func(i, j int) bool { return req.Types[i].Address < req.Types[j].Address })
	sort.Slice(req.Nominals, func(i, j int) bool { return req.Nominals[i].Descriptor < req.Nominals[j].Descriptor })
	sort.Slice(req.Conformances, func(i, j int) bool { return req.Conformances[i] < req.Conformances[j] })
	sort.Strings(req.Accessors)
	sort.Slice(req.Globals, func(i, j int) bool { return req.Globals[i].Name < req.Globals[j].Name })

	if len(req.Types) == 0 {
		return nil, fmt.Errorf("store images have no concrete Swift type records")
	}

	return req, nil
}

func (s *source) relative(address uint64) uint64 {
	return uint64(int64(address) + int64(int32(s.u32(address))))
}

// storeCalls resolves imported branch targets, including shared-cache stubs that
// no longer appear in the individual image's symbol or import tables.
func (s *source) storeCalls() map[uint64]profileformat.Call {
	c := &collector{v: &bootstrapVM{source: s}, calls: map[uint64]profileformat.Call{}}

	for _, name := range []string{amsImage, authImage} {
		for _, sec := range s.images[name].Sections {
			if !sec.Flags.IsPureInstructions() && !sec.Flags.IsSomeInstructions() {
				continue
			}

			data := s.read(sec.Addr, sec.Size)
			for off := uint64(0); off+4 <= sec.Size; off += 4 {
				w := binary.LittleEndian.Uint32(data[off:])
				if w&0x7c000000 != 0x14000000 {
					continue
				}

				imm := int64(w & 0x3ffffff)
				if imm&(1<<25) != 0 {
					imm -= 1 << 26
				}

				c.resolve(uint64(int64(sec.Addr+off) + imm*4))
			}
		}
	}

	return c.calls
}

// referencedPointer recognizes compiler loads from dyld-coalesced GOT slots,
// which may live outside the importing image's own GOT sections.
func referencedPointer(page, load uint32, pc uint64) (uint64, bool) {
	if page&0x9f000000 != 0x90000000 || load&0xffc00000 != 0xf9400000 || page&31 != (load>>5)&31 {
		return 0, false
	}

	return adrp(page, pc) + uint64(load>>10&0xfff)*8, true
}
