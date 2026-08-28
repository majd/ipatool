package machimage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/blacktop/go-macho"
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
	relocated  bool
	loadedBase uint64
}

func Open(name string, input []byte) (*Image, error) {
	data, err := amd64Slice(input)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}

	file, err := macho.NewFile(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open %s Mach-O: %w", name, err)
	}

	if file.CPU != types.CPUAmd64 {
		return nil, fmt.Errorf("open %s: expected x86-64 Mach-O, found %s", name, file.CPU)
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

	image.binds, err = file.GetBindInfo()
	if err != nil {
		return nil, fmt.Errorf("read %s bindings: %w", name, err)
	}

	image.rebases, err = file.GetRebaseInfo()
	if err != nil {
		return nil, fmt.Errorf("read %s rebases: %w", name, err)
	}

	return image, nil
}

func amd64Slice(input []byte) ([]byte, error) {
	fat, err := macho.NewFatFile(bytes.NewReader(input))
	if err != nil {
		return bytes.Clone(input), nil
	}

	for _, architecture := range fat.Arches {
		if architecture.CPU != types.CPUAmd64 {
			continue
		}

		end, overflow := add(uint64(architecture.Offset), uint64(architecture.Size))
		if overflow || end > uint64(len(input)) {
			return nil, errors.New("x86-64 slice exceeds input size")
		}

		return bytes.Clone(input[architecture.Offset:end]), nil
	}

	return nil, errors.New("universal binary has no x86-64 slice")
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

	i.relocated = true
	i.loadedBase = loadBase

	return nil
}

func (i *Image) Load(memory Memory) error {
	if !i.relocated {
		return fmt.Errorf("%s must be relocated before loading", i.name)
	}

	var span uint64

	for _, item := range i.segments {
		if item.name == "__PAGEZERO" || item.size == 0 {
			continue
		}

		if item.address < i.base {
			return fmt.Errorf("segment %s in %s precedes image base", item.name, i.name)
		}

		end, overflow := add(item.address-i.base, item.size)
		if overflow || end > maxImageSpan {
			return fmt.Errorf("segment %s makes %s too large", item.name, i.name)
		}

		span = max(span, end)
	}

	span = align(span, pageSize)
	if span == 0 {
		return fmt.Errorf("%s has no loadable segments", i.name)
	}

	if _, overflow := add(i.loadedBase, span); overflow {
		return fmt.Errorf("load range for %s overflows", i.name)
	}

	if err := memory.MemMap(i.loadedBase, span); err != nil {
		return fmt.Errorf("map %s: %w", i.name, err)
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
