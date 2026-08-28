package unicorn

import "fmt"

var mingwLongjmpPattern = []byte{
	0x13, 0x50, 0x41, 0xa9,
	0x15, 0x58, 0x42, 0xa9,
	0x17, 0x60, 0x43, 0xa9,
	0x19, 0x68, 0x44, 0xa9,
}

func prepareArchitectureLibrary(base, imageSize, imports uintptr) error {
	// Windows' SEH longjmp cannot unwind TCG-generated ARM64 frames. Redirect
	// the import to the compatible implementation already present in the DLL.
	longjmp, err := findImport(base, imageSize, imports, "api-ms-win-crt-private-l1-1-0.dll", "longjmp")
	if err != nil {
		return err
	}
	mingwLongjmp, err := findImagePattern(base, imageSize, mingwLongjmpPattern)
	if err != nil {
		return fmt.Errorf("locate MinGW longjmp implementation: %w", err)
	}
	if err := replaceImport(longjmp, mingwLongjmp); err != nil {
		return fmt.Errorf("replace longjmp import: %w", err)
	}
	return nil
}
