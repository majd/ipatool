package machimage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/blacktop/go-macho"
	"github.com/blacktop/go-macho/pkg/fixupchains"
	"github.com/blacktop/go-macho/types"
)

const (
	pageSize     = uint64(0x1000)
	maxImageSpan = uint64(1 << 30)
	pointerSize  = uint64(8)
)

type Memory interface {
	MemMap(address, size uint64) error
	MemWrite(address uint64, data []byte) error
}

type segment struct {
	name     string
	address  uint64
	size     uint64
	fileOff  uint64
	fileSize uint64
}

type Image struct {
	name       string
	data       []byte
	file       *macho.File
	base       uint64
	segments   []segment
	binds      []types.Bind
	rebases    []types.Rebase
	chained    *fixupchains.DyldChainedFixups
	relocated  bool
	loadedBase uint64
}

func (i *Image) Base() uint64 {
	return i.base
}

//nolint:wsl // Image validation and fixup discovery are clearer in execution order.
func Open(name string, input []byte) (*Image, error) {
	data, err := arm64Slice(input)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}

	file, err := macho.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open %s Mach-O: %w", name, err)
	}

	if file.CPU != types.CPUArm64 {
		return nil, fmt.Errorf("open %s: expected arm64 Mach-O, found %s", name, file.CPU)
	}

	image := &Image{
		name: name,
		data: data,
		file: file,
		base: file.GetBaseAddress(),
	}
	for _, item := range file.Segments() {
		image.segments = append(image.segments, segment{
			name:     item.Name,
			address:  item.Addr,
			size:     item.Memsz,
			fileOff:  item.Offset,
			fileSize: item.Filesz,
		})
	}

	if err := image.validateSegments(); err != nil {
		return nil, err
	}
	if err := image.removePointerAuthentication(); err != nil {
		return nil, err
	}

	if file.HasDyldChainedFixups() {
		image.chained, err = file.DyldChainedFixups()
		if err != nil {
			return nil, fmt.Errorf("read %s chained fixups: %w", name, err)
		}

		if _, err := image.chained.Parse(); err != nil {
			return nil, fmt.Errorf("parse %s chained fixups: %w", name, err)
		}
	} else {
		image.binds, err = file.GetBindInfo()
		if err != nil && !errors.Is(err, macho.ErrMachODyldInfoNotFound) {
			return nil, fmt.Errorf("read %s bindings: %w", name, err)
		}

		image.rebases, err = file.GetRebaseInfo()
		if err != nil && !errors.Is(err, macho.ErrMachODyldInfoNotFound) {
			return nil, fmt.Errorf("read %s rebases: %w", name, err)
		}
	}

	return image, nil
}

//nolint:wsl // Instruction decoding is clearer when related operations stay together.
func (i *Image) removePointerAuthentication() error {
	const nop = uint32(0xd503201f)

	for _, section := range i.file.Sections {
		if !section.Flags.IsPureInstructions() && !section.Flags.IsSomeInstructions() {
			continue
		}
		start := uint64(section.Offset)
		end, overflow := add(start, section.Size)
		if overflow || end > uint64(len(i.data)) {
			return fmt.Errorf("instruction section %s exceeds %s", section.Name, i.name)
		}
		for offset := start; offset+4 <= end; offset += 4 {
			instruction := binary.LittleEndian.Uint32(i.data[offset:])
			var replacement uint32

			switch instruction & 0xfffffc00 {
			case 0xd71f0800, 0xd71f0c00, 0xd61f0800, 0xd61f0c00:
				replacement = 0xd61f0000 | (instruction & 0x3e0) // br xN
			case 0xd73f0800, 0xd73f0c00, 0xd63f0800, 0xd63f0c00:
				replacement = 0xd63f0000 | (instruction & 0x3e0) // blr xN
			case 0xdac10000, 0xdac10400, 0xdac10800, 0xdac10c00,
				0xdac11000, 0xdac11400, 0xdac11800, 0xdac11c00:
				replacement = nop
			default:
				switch instruction {
				case 0xd65f0bff, 0xd65f0fff:
					replacement = 0xd65f03c0 // ret
				case 0xd503211f, 0xd503215f, 0xd503219f, 0xd50321df,
					0xd503233f, 0xd503237f, 0xd50323bf, 0xd50323ff,
					0xd50320ff:
					replacement = nop
				default:
					opcode := instruction & 0xffffffe0
					if opcode == 0xdac123e0 || opcode == 0xdac127e0 ||
						opcode == 0xdac12be0 || opcode == 0xdac12fe0 ||
						opcode == 0xdac133e0 || opcode == 0xdac137e0 ||
						opcode == 0xdac13be0 || opcode == 0xdac13fe0 ||
						opcode == 0xdac143e0 || opcode == 0xdac147e0 {
						replacement = nop
					}
				}
			}
			if replacement != 0 {
				binary.LittleEndian.PutUint32(i.data[offset:], replacement)
			}
		}
	}

	return nil
}

func arm64Slice(input []byte) ([]byte, error) {
	fat, err := macho.NewFatFile(bytes.NewReader(input))
	if err != nil {
		return bytes.Clone(input), nil
	}

	for _, architecture := range fat.Arches {
		if architecture.CPU != types.CPUArm64 || architecture.SubCPU&types.CpuSubtypeMask != types.CPUSubtypeArm64E {
			continue
		}

		end, overflow := add(uint64(architecture.Offset), uint64(architecture.Size))
		if overflow || end > uint64(len(input)) {
			return nil, errors.New("arm64e slice exceeds input size")
		}

		return bytes.Clone(input[architecture.Offset:end]), nil
	}

	return nil, errors.New("universal binary has no arm64e slice")
}

func (i *Image) Export(name string, loadBase uint64) (uint64, error) {
	address, err := i.file.FindSymbolAddress(name)
	if err != nil {
		return 0, fmt.Errorf("find %s in %s: %w", name, i.name, err)
	}

	if address < i.base {
		return 0, fmt.Errorf("symbol %s in %s precedes image base", name, i.name)
	}

	result, overflow := add(loadBase, address-i.base)
	if overflow {
		return 0, fmt.Errorf("symbol %s address overflows in %s", name, i.name)
	}

	return result, nil
}

func (i *Image) Relocate(loadBase uint64, resolve func(string) (uint64, error)) error {
	if i.relocated {
		return fmt.Errorf("%s is already relocated", i.name)
	}

	for _, relocation := range i.rebases {
		if relocation.Type != types.REBASE_TYPE_POINTER {
			return fmt.Errorf("%s uses unsupported rebase type %d", i.name, relocation.Type)
		}

		if relocation.Value < i.base {
			return fmt.Errorf("%s contains a rebase below its image base", i.name)
		}

		offset, err := i.segmentFileOffset(relocation.Segment, relocation.Offset, pointerSize)
		if err != nil {
			return err
		}

		address, overflow := add(loadBase, relocation.Value-i.base)
		if overflow {
			return fmt.Errorf("rebase address overflows in %s", i.name)
		}

		if err := i.putPointer(offset, address); err != nil {
			return err
		}
	}

	for _, binding := range i.binds {
		// Lazy bind streams may omit SET_TYPE; dyld's default is a pointer bind.
		if binding.Type != 0 && binding.Type != types.BIND_TYPE_POINTER {
			return fmt.Errorf("%s uses unsupported bind type %d for %s", i.name, binding.Type, binding.Name)
		}

		offset, err := i.segmentFileOffset(binding.Segment, binding.SegOffset, pointerSize)
		if err != nil {
			return err
		}

		address, err := resolve(binding.Name)
		if err != nil {
			return fmt.Errorf("resolve %s for %s: %w", binding.Name, i.name, err)
		}

		address, err = addSigned(address, binding.Addend)
		if err != nil {
			return fmt.Errorf("apply addend for %s in %s: %w", binding.Name, i.name, err)
		}

		if err := i.putPointer(offset, address); err != nil {
			return err
		}
	}

	if err := i.relocateChained(loadBase, resolve); err != nil {
		return err
	}

	i.relocated = true
	i.loadedBase = loadBase

	return nil
}

//nolint:wsl // Chained-fixup decoding is clearer when related operations stay together.
func (i *Image) relocateChained(loadBase uint64, resolve func(string) (uint64, error)) error {
	if i.chained == nil {
		return nil
	}

	for _, start := range i.chained.Starts {
		for _, fixup := range start.Fixups {
			offset := fixup.Offset()
			if offset > uint64(len(i.data)) || pointerSize > uint64(len(i.data))-offset {
				return fmt.Errorf("chained fixup at %#x exceeds %s", offset, i.name)
			}

			var address uint64

			switch {
			case fixup.IsRebase():
				var target uint64
				switch rebase := fixup.(type) {
				case fixupchains.DyldChainedPtr64RebaseOffset:
					// DYLD_CHAINED_PTR_64_OFFSET encodes an image-relative
					// target. go-macho's Rebase currently subtracts the
					// preferred address from that offset a second time.
					target = rebase.UnpackedTarget()
				case fixupchains.DyldChainedPtrArm64eRebase:
					target = rebase.Target()

					if i.chained.PointerFormat == fixupchains.DYLD_CHAINED_PTR_ARM64E {
						absolute := rebase.UnpackTarget() & 0x00ffffffffffffff
						if absolute < i.base {
							return fmt.Errorf("chained rebase at %#x precedes the base of %s", offset, i.name)
						}
						target = absolute - i.base
					}
				case fixupchains.DyldChainedPtrArm64eAuthRebase:
					target = rebase.Target()
				default:
					var err error

					target, err = i.chained.Rebase(offset, i.base)
					if err != nil {
						return fmt.Errorf("decode chained rebase at %#x in %s: %w", offset, i.name, err)
					}
				}

				var overflow bool

				address, overflow = add(loadBase, target)
				if overflow {
					return fmt.Errorf("chained rebase at %#x with target %#x, image base %#x, load base %#x, format %s, and type %T overflows in %s", offset, target, i.base, loadBase, i.chained.PointerFormat, fixup, i.name)
				}
			case fixup.IsBind():
				binding, ok := fixup.(fixupchains.Bind)
				if !ok {
					return fmt.Errorf("invalid chained bind at %#x in %s", offset, i.name)
				}

				var err error

				address, err = resolve(binding.Name())
				if err != nil {
					return fmt.Errorf("resolve %s for %s: %w", binding.Name(), i.name, err)
				}

				addend := int64(binding.Addend())
				switch arm64e := fixup.(type) {
				case fixupchains.DyldChainedPtrArm64eBind:
					addend = arm64e.SignExtendedAddend()
				case fixupchains.DyldChainedPtrArm64eBind24:
					addend = arm64e.SignExtendedAddend()
				}
				if ordinal := binding.Ordinal(); ordinal < uint64(len(i.chained.Imports)) {
					addend += int64(i.chained.Imports[ordinal].Addend())
				}

				address, err = addSigned(address, addend)
				if err != nil {
					return fmt.Errorf("apply chained addend for %s in %s: %w", binding.Name(), i.name, err)
				}
			default:
				return fmt.Errorf("unknown chained fixup at %#x in %s", offset, i.name)
			}

			if err := i.putPointer(offset, address); err != nil {
				return err
			}
		}
	}

	return nil
}

// PreparePrelinked prepares an image extracted from a dyld shared cache. Such
// images have already had their cache slide information applied, so they must
// remain at their preferred addresses. external maps cache symbol addresses to
// the import names implemented by the emulator.
func (i *Image) PreparePrelinked(external map[uint64]string, resolve func(string) (uint64, error)) error {
	if i.relocated {
		return fmt.Errorf("%s is already relocated", i.name)
	}

	patched := make(map[uint64]uint64, len(external))

	for address, name := range external {
		value, err := resolve(name)
		if err != nil {
			return fmt.Errorf("resolve %s for %s: %w", name, i.name, err)
		}

		patched[address] = value
	}

	for _, item := range i.segments {
		if item.name == "__TEXT" || item.name == "__LINKEDIT" || item.fileSize < pointerSize {
			continue
		}

		start := item.fileOff
		if remainder := item.address % pointerSize; remainder != 0 {
			start += pointerSize - remainder
		}

		end := item.fileOff + item.fileSize
		for offset := start; offset <= end-pointerSize; offset += pointerSize {
			current := binary.LittleEndian.Uint64(i.data[offset : offset+pointerSize])

			value, ok := patched[current]
			if !ok {
				continue
			}

			if err := i.putPointer(offset, value); err != nil {
				return err
			}
		}
	}

	i.relocated = true
	i.loadedBase = i.base

	return nil
}

func (i *Image) Load(memory Memory) error {
	if !i.relocated {
		return fmt.Errorf("%s must be relocated before loading", i.name)
	}

	type memoryRange struct {
		start uint64
		end   uint64
	}

	ranges := make([]memoryRange, 0, len(i.segments))

	for _, item := range i.segments {
		if item.name == "__PAGEZERO" || item.size == 0 {
			continue
		}

		if item.address < i.base {
			return fmt.Errorf("segment %s in %s precedes image base", item.name, i.name)
		}

		address, overflow := add(i.loadedBase, item.address-i.base)
		if overflow {
			return fmt.Errorf("segment %s address overflows in %s", item.name, i.name)
		}

		end, overflow := add(address, item.size)
		if overflow {
			return fmt.Errorf("segment %s range overflows in %s", item.name, i.name)
		}

		ranges = append(ranges, memoryRange{start: address - address%pageSize, end: align(end, pageSize)})
	}

	if len(ranges) == 0 {
		return fmt.Errorf("%s has no loadable segments", i.name)
	}

	sort.Slice(ranges, func(left, right int) bool { return ranges[left].start < ranges[right].start })
	merged := ranges[:0]

	for _, current := range ranges {
		last := len(merged) - 1
		if last >= 0 && current.start <= merged[last].end {
			merged[last].end = max(merged[last].end, current.end)

			continue
		}

		merged = append(merged, current)
	}

	var mapped uint64

	for _, item := range merged {
		size := item.end - item.start

		mapped, _ = add(mapped, size)
		if mapped > maxImageSpan {
			return fmt.Errorf("mapped segments make %s too large", i.name)
		}

		if err := memory.MemMap(item.start, size); err != nil {
			return fmt.Errorf("map %s: %w", i.name, err)
		}
	}

	for _, item := range i.segments {
		if item.name == "__PAGEZERO" || item.fileSize == 0 {
			continue
		}

		end, overflow := add(item.fileOff, item.fileSize)
		if overflow || end > uint64(len(i.data)) {
			return fmt.Errorf("segment %s data exceeds %s", item.name, i.name)
		}

		address, overflow := add(i.loadedBase, item.address-i.base)
		if overflow {
			return fmt.Errorf("segment %s address overflows in %s", item.name, i.name)
		}

		if err := memory.MemWrite(address, i.data[item.fileOff:end]); err != nil {
			return fmt.Errorf("write segment %s from %s: %w", item.name, i.name, err)
		}
	}

	return nil
}

func (i *Image) validateSegments() error {
	for _, item := range i.segments {
		if item.fileSize > item.size {
			return fmt.Errorf("segment %s file data exceeds its memory size in %s", item.name, i.name)
		}

		if _, overflow := add(item.address, item.size); overflow {
			return fmt.Errorf("segment %s address range overflows in %s", item.name, i.name)
		}

		end, overflow := add(item.fileOff, item.fileSize)
		if overflow || end > uint64(len(i.data)) {
			return fmt.Errorf("segment %s data exceeds %s", item.name, i.name)
		}
	}

	return nil
}

func (i *Image) segmentFileOffset(name string, offset, size uint64) (uint64, error) {
	for _, item := range i.segments {
		if item.name != name {
			continue
		}

		segmentEnd, overflow := add(offset, size)
		if overflow || segmentEnd > item.size {
			return 0, fmt.Errorf("fixup at %#x exceeds segment %s in %s", offset, name, i.name)
		}

		if segmentEnd > item.fileSize {
			return 0, fmt.Errorf("fixup at %#x exceeds file data for segment %s in %s", offset, name, i.name)
		}

		result, overflow := add(item.fileOff, offset)
		if overflow {
			return 0, fmt.Errorf("fixup offset overflows in %s", i.name)
		}

		end, overflow := add(result, size)
		if overflow || end > uint64(len(i.data)) {
			return 0, fmt.Errorf("fixup at %#x exceeds %s", result, i.name)
		}

		return result, nil
	}

	return 0, fmt.Errorf("fixup references unknown segment %s in %s", name, i.name)
}

func (i *Image) putPointer(offset, value uint64) error {
	end, overflow := add(offset, 8)
	if overflow || end > uint64(len(i.data)) {
		return fmt.Errorf("fixup at %#x exceeds %s", offset, i.name)
	}

	binary.LittleEndian.PutUint64(i.data[offset:end], value)

	return nil
}

func addSigned(value uint64, delta int64) (uint64, error) {
	if delta >= 0 {
		result, overflow := add(value, uint64(delta))
		if overflow {
			return 0, errors.New("address overflow")
		}

		return result, nil
	}

	magnitude := uint64(-(delta + 1)) + 1
	if magnitude > value {
		return 0, errors.New("address underflow")
	}

	return value - magnitude, nil
}

func add(left, right uint64) (uint64, bool) {
	return left + right, right > math.MaxUint64-left
}

func align(value, alignment uint64) uint64 {
	return (value + alignment - 1) &^ (alignment - 1)
}
