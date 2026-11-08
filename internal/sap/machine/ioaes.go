package machine

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
)

const commerceAESHandle = uint32(0x89c)

// emulateIOAES evaluates the device-key handle used by the pinned FairPlay
// runtime without delegating to the host operating system.
func emulateIOAES(source, iv []byte, operation uint32, keyData []byte) ([]byte, []byte, error) {
	if len(source) == 0 || len(source)%aes.BlockSize != 0 || len(iv) != aes.BlockSize || len(keyData) != 40 {
		return nil, nil, errors.New("invalid IOAES parameters")
	}

	handle := binary.LittleEndian.Uint32(keyData[36:40])
	if handle != commerceAESHandle {
		return nil, nil, fmt.Errorf("unsupported IOAES key handle %#x", handle)
	}

	block, err := aes.NewCipher(make([]byte, aes.BlockSize))
	if err != nil {
		return nil, nil, fmt.Errorf("initialize IOAES cipher: %w", err)
	}

	destination := make([]byte, len(source))
	outputIV := make([]byte, aes.BlockSize)

	switch operation {
	case 0:
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(destination, source)
		copy(outputIV, destination[len(destination)-aes.BlockSize:])
	case 1:
		copy(outputIV, source[len(source)-aes.BlockSize:])
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(destination, source)
	default:
		return nil, nil, fmt.Errorf("unsupported IOAES operation %#x", operation)
	}

	return destination, outputIV, nil
}
