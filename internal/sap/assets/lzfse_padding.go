package assets

import (
	"encoding/binary"
	"errors"
)

// padLZFSEStreams supplies unused low accumulator bytes for short FSE streams.
// Apple's decoder can read those bytes before the payload; the pure Go decoder
// bounds each stream separately and otherwise rejects valid short payloads.
// This is only used on authenticated AEA data, whose decoded SHA-256 is checked.
func padLZFSEStreams(src []byte) ([]byte, error) {
	var out []byte

	for len(src) >= 4 {
		magic := binary.LittleEndian.Uint32(src)
		if magic == 0x24787662 {
			return append(out, src...), nil
		}

		if len(src) < 12 {
			return nil, errors.New("short LZFSE block")
		}

		if magic == 0x2d787662 || magic == 0x6e787662 {
			size := 8 + int(binary.LittleEndian.Uint32(src[4:]))
			if magic == 0x6e787662 {
				size = 12 + int(binary.LittleEndian.Uint32(src[8:]))
			}

			if size > len(src) {
				return nil, errors.New("truncated LZFSE block")
			}

			out = append(out, src[:size]...)
			src = src[size:]

			continue
		}

		var header, literals, matches int

		var v0, v1 uint64

		switch magic {
		case 0x32787662:
			if len(src) < 32 {
				return nil, errors.New("short LZFSE V2 header")
			}

			v0 = binary.LittleEndian.Uint64(src[8:])
			v1 = binary.LittleEndian.Uint64(src[16:])
			header = int(binary.LittleEndian.Uint32(src[24:]))
			literals = int(v0 >> 20 & 0xfffff)
			matches = int(v1 >> 40 & 0xfffff)

			if header < 32 {
				return nil, errors.New("invalid LZFSE V2 header")
			}
		case 0x31787662:
			header = 772
			if len(src) < header {
				return nil, errors.New("short LZFSE V1 header")
			}

			literals = int(binary.LittleEndian.Uint32(src[20:]))
			matches = int(binary.LittleEndian.Uint32(src[24:]))
		default:
			return nil, errors.New("unknown LZFSE block")
		}

		if header > len(src) || literals > len(src)-header || matches > len(src)-header-literals {
			return nil, errors.New("truncated LZFSE payload")
		}

		lp, mp := max(0, 8-literals), max(0, 8-matches)

		h := append([]byte(nil), src[:header]...)

		if magic == 0x32787662 {
			binary.LittleEndian.PutUint64(h[8:], v0&^(uint64(0xfffff)<<20)|uint64(literals+lp)<<20)
			binary.LittleEndian.PutUint64(h[16:], v1&^(uint64(0xfffff)<<40)|uint64(matches+mp)<<40)
		} else {
			binary.LittleEndian.PutUint32(h[8:], uint32(literals+lp+matches+mp))
			binary.LittleEndian.PutUint32(h[20:], uint32(literals+lp))
			binary.LittleEndian.PutUint32(h[24:], uint32(matches+mp))
		}

		out = append(out, h...)
		out = append(out, make([]byte, lp)...)
		out = append(out, src[header:header+literals]...)
		out = append(out, make([]byte, mp)...)
		out = append(out, src[header+literals:header+literals+matches]...)
		src = src[header+literals+matches:]
	}

	return nil, errors.New("missing LZFSE end marker")
}
