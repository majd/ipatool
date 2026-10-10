// Package lzfse provides the decoder API used by go-apfs without requiring CGo.
package lzfse

import pure "github.com/go-compressions/lzfse"

// DecodeBuffer decompresses an LZFSE stream. Invalid streams return nil.
func DecodeBuffer(encoded []byte) []byte {
	decoded, err := pure.Decompress(encoded)
	if err != nil {
		return nil
	}

	return decoded
}

// DecodeBufferInto decompresses an LZFSE stream into decoded and returns the
// number of bytes written. A zero result indicates failure or insufficient
// output space.
func DecodeBufferInto(encoded, decoded []byte) int {
	data, err := pure.Decompress(encoded)
	if err != nil || len(data) > len(decoded) {
		return 0
	}

	return copy(decoded, data)
}

// DecodeLZVNBuffer decompresses a raw LZVN stream into decoded and returns the
// number of bytes written. A zero result indicates failure.
func DecodeLZVNBuffer(encoded, decoded []byte) uint {
	data, err := pure.DecompressLZVN(encoded, len(decoded))
	if err != nil {
		return 0
	}

	return uint(copy(decoded, data))
}
