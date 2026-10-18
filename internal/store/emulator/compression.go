package emulator

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/binary"
	"io"
)

type inflateStream struct {
	bits          int64
	input, output []byte
	position      int
	decoded       bool
}

func (r *Runtime) write32(address uint64, value uint32) {
	var b [4]byte

	binary.LittleEndian.PutUint32(b[:], value)

	if err := r.MemWrite(address, b[:]); err != nil {
		panic(err)
	}
}
func (r *Runtime) compressionSymbol(name string) bool {
	switch name {
	case "inflateInit2_":
		if r.arg(3) != 112 {
			panic("unsupported zlib stream ABI")
		}

		if r.inflaters == nil {
			r.inflaters = map[uint64]*inflateStream{}
		}

		r.inflaters[r.arg(0)] = &inflateStream{bits: int64(int32(r.arg(1)))}
		r.write64(r.arg(0)+16, 0)
		r.write64(r.arg(0)+40, 0)
		r.set(0)
	case "inflateEnd":
		delete(r.inflaters, r.arg(0))
		r.set(0)
	case "inflate":
		address := r.arg(0)

		state := r.inflaters[address]
		if state == nil {
			panic("uninitialized inflate stream")
		}

		size := uint64(uint32(r.read64(address + 8)))
		if size > 32<<20 {
			panic("inflate input exceeds budget")
		}

		if size != 0 {
			p := r.read64(address)

			data, err := r.engine.MemRead(p, size)
			if err != nil {
				panic(err)
			}

			state.input = append(state.input, data...)

			r.write64(address, p+size)
			r.write32(address+8, 0)
			r.write64(address+16, r.read64(address+16)+size)
		}

		if !state.decoded {
			var reader io.ReadCloser

			var err error

			switch state.bits {
			case 31, 47:
				reader, err = gzip.NewReader(bytes.NewReader(state.input))
			case 15:
				reader, err = zlib.NewReader(bytes.NewReader(state.input))
			case -15:
				reader = flate.NewReader(bytes.NewReader(state.input))
			default:
				panic("unsupported inflate window size")
			}

			if err != nil {
				r.set(^uint64(2))

				return true
			}

			state.output, err = io.ReadAll(io.LimitReader(reader, (32<<20)+1))
			_ = reader.Close()

			if err != nil {
				r.set(^uint64(2))

				return true
			}

			if len(state.output) > 32<<20 {
				panic("decompressed response exceeds runtime limit")
			}

			state.decoded = true
		}

		size = uint64(uint32(r.read64(address + 32)))
		count := min(size, uint64(len(state.output)-state.position))
		out := r.read64(address + 24)

		if count != 0 {
			if err := r.MemWrite(out, state.output[state.position:state.position+int(count)]); err != nil {
				panic(err)
			}
		}

		state.position += int(count)
		r.write64(address+24, out+count)
		r.write32(address+32, uint32(size-count))
		r.write64(address+40, r.read64(address+40)+count)
		r.set(boolValue(state.position == len(state.output)))
	default:
		return false
	}

	return true
}
