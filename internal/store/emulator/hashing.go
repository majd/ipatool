package emulator

import "math/bits"

// Swift's Hasher uses SipHash-1-3 with a 72-byte ABI buffer. This implements the
// runtime primitive used by guest dictionaries; no Store signature uses it.
func (r *Runtime) initHasher(address, seed uint64) {
	var a, b uint64
	if p := r.profile.Symbols["_swift_stdlib_Hashing_parameters"]; p != 0 {
		a = r.read64(p)
		b = r.read64(p + 8)
	}

	a ^= seed

	values := []uint64{0, 0x736f6d6570736575 ^ a, 0x646f72616e646f6d ^ b, 0x6c7967656e657261 ^ a, 0x7465646279746573 ^ b, 0, 0, 0, 0}
	for i, v := range values {
		r.write64(address+uint64(i)*8, v)
	}
}
func sipRound(v *[4]uint64) {
	v[0] += v[1]
	v[1] = bits.RotateLeft64(v[1], 13) ^ v[0]
	v[0] = bits.RotateLeft64(v[0], 32)
	v[2] += v[3]
	v[3] = bits.RotateLeft64(v[3], 16) ^ v[2]
	v[0] += v[3]
	v[3] = bits.RotateLeft64(v[3], 21) ^ v[0]
	v[2] += v[1]
	v[1] = bits.RotateLeft64(v[1], 17) ^ v[2]
	v[2] = bits.RotateLeft64(v[2], 32)
}
func (r *Runtime) hashCompress(address, word uint64, final bool) uint64 {
	var v [4]uint64
	for i := range v {
		v[i] = r.read64(address + 8 + uint64(i)*8)
	}

	v[3] ^= word
	sipRound(&v)

	v[0] ^= word
	if final {
		v[2] ^= 255
		for i := 0; i < 3; i++ {
			sipRound(&v)
		}
	}

	for i, w := range v {
		r.write64(address+8+uint64(i)*8, w)
	}

	return v[0] ^ v[1] ^ v[2] ^ v[3]
}
func (r *Runtime) hashWord(address, word uint64) {
	tail := r.read64(address)
	count := tail >> 56
	shift := (count & 7) * 8
	chunk := word

	if shift != 0 {
		chunk = (tail & 0xffffffffffffff) | (word << shift)
		tail = ((count + 8) << 56) | (word >> (64 - shift))
	} else {
		tail += (8 << 56)
	}

	r.write64(address, tail)
	r.hashCompress(address, chunk, false)
}

func (r *Runtime) hashBytes(address uint64, data []byte) {
	for _, b := range data {
		tail := r.read64(address)
		count := tail >> 56
		shift := (count & 7) * 8
		word := (tail & 0xffffffffffffff) | (uint64(b) << shift)

		if shift == 56 {
			r.hashCompress(address, word, false)
			r.write64(address, (count+1)<<56)
		} else {
			r.write64(address, ((count+1)<<56)|word)
		}
	}
}
