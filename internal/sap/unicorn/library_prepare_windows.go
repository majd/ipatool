package unicorn

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const (
	peSignature            = 0x00004550
	pe32PlusMagic          = 0x020b
	peImportDirectory      = 1
	peOptionalHeaderOffset = 24
	peDataDirectoryOffset  = 112
	peSizeOfImageOffset    = 56
	peImportDescriptorSize = 20
	peOrdinalFlag64        = uint64(1 << 63)
	memCommit              = 0x1000
	memReserve             = 0x2000
	pageReadWrite          = 0x04
	pageExecuteReadWrite   = 0x40
)

var (
	prepareLibraryMu     sync.Mutex
	originalVirtualAlloc atomic.Uintptr
	virtualAllocHook     = syscall.NewCallback(commitVirtualAlloc)
	virtualProtect       = syscall.NewLazyDLL("kernel32.dll").NewProc("VirtualProtect")
)

func prepareLibrary(handle syscall.Handle) error {
	prepareLibraryMu.Lock()
	defer prepareLibraryMu.Unlock()

	base := uintptr(handle)
	imageSize, imports, err := peImports(base)
	if err != nil {
		return err
	}
	virtualAlloc, err := findImport(base, imageSize, imports, "kernel32.dll", "VirtualAlloc")
	if err != nil {
		return err
	}
	currentVirtualAlloc := *(*uintptr)(unsafe.Pointer(virtualAlloc))
	if currentVirtualAlloc != virtualAllocHook {
		known := originalVirtualAlloc.Load()
		if known != 0 && known != currentVirtualAlloc {
			return errors.New("VirtualAlloc import changed unexpectedly")
		}
		originalVirtualAlloc.Store(currentVirtualAlloc)
		if err := replaceImport(virtualAlloc, virtualAllocHook); err != nil {
			return fmt.Errorf("replace VirtualAlloc import: %w", err)
		}
	} else if originalVirtualAlloc.Load() == 0 {
		return errors.New("VirtualAlloc hook has no original function")
	}
	return prepareArchitectureLibrary(base, imageSize, imports)
}

func peImports(base uintptr) (uintptr, uintptr, error) {
	if readUint16(base) != 0x5a4d {
		return 0, 0, errors.New("invalid DOS header")
	}
	nt := base + uintptr(readUint32(base+0x3c))
	if readUint32(nt) != peSignature {
		return 0, 0, errors.New("invalid PE header")
	}
	optional := nt + peOptionalHeaderOffset
	if readUint16(optional) != pe32PlusMagic {
		return 0, 0, errors.New("Unicorn is not a 64-bit PE image")
	}
	imageSize := uintptr(readUint32(optional + peSizeOfImageOffset))
	importRVA := uintptr(readUint32(optional + peDataDirectoryOffset + peImportDirectory*8))
	if imageSize == 0 || importRVA == 0 {
		return 0, 0, errors.New("PE import directory is missing")
	}
	imports := base + importRVA
	if err := requireImageRange(base, imageSize, imports, peImportDescriptorSize); err != nil {
		return 0, 0, err
	}
	return imageSize, imports, nil
}

func findImport(base, imageSize, imports uintptr, dll, function string) (uintptr, error) {
	for descriptor := imports; ; descriptor += peImportDescriptorSize {
		if err := requireImageRange(base, imageSize, descriptor, peImportDescriptorSize); err != nil {
			return 0, err
		}
		nameRVA := readUint32(descriptor + 12)
		if nameRVA == 0 {
			break
		}
		name, err := readImageString(base, imageSize, uintptr(nameRVA))
		if err != nil {
			return 0, err
		}
		if !strings.EqualFold(name, dll) {
			continue
		}

		namesRVA := uintptr(readUint32(descriptor))
		addressesRVA := uintptr(readUint32(descriptor + 16))
		if namesRVA == 0 || addressesRVA == 0 {
			break
		}
		for index := uintptr(0); ; index++ {
			nameSlot := base + namesRVA + index*8
			addressSlot := base + addressesRVA + index*8
			if err := requireImageRange(base, imageSize, nameSlot, 8); err != nil {
				return 0, err
			}
			if err := requireImageRange(base, imageSize, addressSlot, 8); err != nil {
				return 0, err
			}
			nameEntry := readUint64(nameSlot)
			if nameEntry == 0 {
				break
			}
			if nameEntry&peOrdinalFlag64 != 0 {
				continue
			}
			name, err := readImageString(base, imageSize, uintptr(nameEntry)+2)
			if err != nil {
				return 0, err
			}
			if name == function {
				return addressSlot, nil
			}
		}
	}
	return 0, fmt.Errorf("%s!%s import was not found", dll, function)
}

func findImagePattern(base, imageSize uintptr, pattern []byte) (uintptr, error) {
	image := unsafe.Slice((*byte)(unsafe.Pointer(base)), imageSize)
	first := bytes.Index(image, pattern)
	if first < 0 {
		return 0, errors.New("instruction pattern was not found")
	}
	if bytes.Index(image[first+len(pattern):], pattern) >= 0 {
		return 0, errors.New("instruction pattern is not unique")
	}
	return base + uintptr(first), nil
}

func replaceImport(address, replacement uintptr) error {
	if *(*uintptr)(unsafe.Pointer(address)) == replacement {
		return nil
	}
	var oldProtection uint32
	result, _, callErr := virtualProtect.Call(address, unsafe.Sizeof(replacement), pageReadWrite, uintptr(unsafe.Pointer(&oldProtection)))
	if result == 0 {
		return fmt.Errorf("make import table writable: %w", callErr)
	}
	*(*uintptr)(unsafe.Pointer(address)) = replacement
	var ignored uint32
	result, _, restoreErr := virtualProtect.Call(address, unsafe.Sizeof(replacement), uintptr(oldProtection), uintptr(unsafe.Pointer(&ignored)))
	if result == 0 {
		return fmt.Errorf("restore import table protection: %w", restoreErr)
	}
	return nil
}

func commitVirtualAlloc(address, size, allocationType, protection uintptr) uintptr {
	if allocationType == memReserve && protection == pageExecuteReadWrite {
		allocationType |= memCommit
	}
	result, _, _ := syscall.SyscallN(originalVirtualAlloc.Load(), address, size, allocationType, protection)
	return result
}

func requireImageRange(base, imageSize, address, size uintptr) error {
	if address < base || address-base > imageSize || size > imageSize-(address-base) {
		return errors.New("PE import table extends beyond the image")
	}
	return nil
}

func readImageString(base, imageSize, rva uintptr) (string, error) {
	if rva >= imageSize {
		return "", errors.New("PE string extends beyond the image")
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(base+rva)), imageSize-rva)
	for index, value := range data {
		if value == 0 {
			return string(data[:index]), nil
		}
	}
	return "", errors.New("PE string is not terminated")
}

func readUint16(address uintptr) uint16 {
	return binary.LittleEndian.Uint16(unsafe.Slice((*byte)(unsafe.Pointer(address)), 2))
}

func readUint32(address uintptr) uint32 {
	return binary.LittleEndian.Uint32(unsafe.Slice((*byte)(unsafe.Pointer(address)), 4))
}

func readUint64(address uintptr) uint64 {
	return binary.LittleEndian.Uint64(unsafe.Slice((*byte)(unsafe.Pointer(address)), 8))
}
