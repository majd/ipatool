package unicorn

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	memRelease      = 0x8000
	pageExecuteRead = 0x20
)

var (
	getCurrentProcess     = syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentProcess")
	flushInstructionCache = syscall.NewLazyDLL("kernel32.dll").NewProc("FlushInstructionCache")
	virtualFree           = syscall.NewLazyDLL("kernel32.dll").NewProc("VirtualFree")
	amd64LongjmpAddress   uintptr
)

// amd64Longjmp restores the nonvolatile state saved by the Windows x64
// _setjmp function without invoking the system unwinder. Unicorn saves a null
// frame specifically because its generated code has no Windows unwind data.
var amd64Longjmp = []byte{
	0x89, 0xd0, 0x85, 0xc0, 0x75, 0x02, 0xff, 0xc0, 0x0f, 0xae, 0x51, 0x58,
	0xd9, 0x69, 0x5c, 0xf3, 0x0f, 0x6f, 0x71, 0x60, 0xf3, 0x0f, 0x6f, 0x79,
	0x70, 0xf3, 0x44, 0x0f, 0x6f, 0x81, 0x80, 0x00, 0x00, 0x00, 0xf3, 0x44,
	0x0f, 0x6f, 0x89, 0x90, 0x00, 0x00, 0x00, 0xf3, 0x44, 0x0f, 0x6f, 0x91,
	0xa0, 0x00, 0x00, 0x00, 0xf3, 0x44, 0x0f, 0x6f, 0x99, 0xb0, 0x00, 0x00,
	0x00, 0xf3, 0x44, 0x0f, 0x6f, 0xa1, 0xc0, 0x00, 0x00, 0x00, 0xf3, 0x44,
	0x0f, 0x6f, 0xa9, 0xd0, 0x00, 0x00, 0x00, 0xf3, 0x44, 0x0f, 0x6f, 0xb1,
	0xe0, 0x00, 0x00, 0x00, 0xf3, 0x44, 0x0f, 0x6f, 0xb9, 0xf0, 0x00, 0x00,
	0x00, 0x48, 0x8b, 0x59, 0x08, 0x48, 0x8b, 0x69, 0x18, 0x48, 0x8b, 0x71,
	0x20, 0x48, 0x8b, 0x79, 0x28, 0x4c, 0x8b, 0x61, 0x30, 0x4c, 0x8b, 0x69,
	0x38, 0x4c, 0x8b, 0x71, 0x40, 0x4c, 0x8b, 0x79, 0x48, 0x4c, 0x8b, 0x59,
	0x50, 0x48, 0x8b, 0x61, 0x10, 0x41, 0xff, 0xe3,
}

func prepareArchitectureLibrary(base, imageSize, imports uintptr) error {
	longjmp, err := findImport(base, imageSize, imports, "msvcrt.dll", "longjmp")
	if err != nil {
		return err
	}
	replacement, err := allocateAMD64Longjmp()
	if err != nil {
		return err
	}
	if err := replaceImport(longjmp, replacement); err != nil {
		return fmt.Errorf("replace longjmp import: %w", err)
	}
	return nil
}

func allocateAMD64Longjmp() (uintptr, error) {
	if amd64LongjmpAddress != 0 {
		return amd64LongjmpAddress, nil
	}

	address, _, allocErr := syscall.SyscallN(
		originalVirtualAlloc.Load(),
		0,
		uintptr(len(amd64Longjmp)),
		memReserve|memCommit,
		pageReadWrite,
	)
	if address == 0 {
		return 0, fmt.Errorf("allocate compatible longjmp: %w", allocErr)
	}
	release := func() {
		_, _, _ = virtualFree.Call(address, 0, memRelease)
	}

	copy(unsafe.Slice((*byte)(unsafe.Pointer(address)), len(amd64Longjmp)), amd64Longjmp)

	var oldProtection uint32
	result, _, protectErr := virtualProtect.Call(
		address,
		uintptr(len(amd64Longjmp)),
		pageExecuteRead,
		uintptr(unsafe.Pointer(&oldProtection)),
	)
	if result == 0 {
		release()
		return 0, fmt.Errorf("make compatible longjmp executable: %w", protectErr)
	}

	process, _, _ := getCurrentProcess.Call()
	result, _, flushErr := flushInstructionCache.Call(process, address, uintptr(len(amd64Longjmp)))
	if result == 0 {
		release()
		return 0, fmt.Errorf("flush compatible longjmp instructions: %w", flushErr)
	}

	amd64LongjmpAddress = address
	return address, nil
}
