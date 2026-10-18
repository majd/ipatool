package profilegen

import "encoding/binary"

// pageReference tracks an ADRP-derived address within a basic block. Unresolved
// uses retain the old conservative window; understood address arithmetic and
// memory operands retain only the pages they actually address.
type pageReference struct {
	address  uint64
	valid    bool
	resolved bool
}

// dataReferences decodes immediate ARM64 address construction without executing
// a particular operation. Every basic block in both Store images is scanned, so
// collection also covers server-dependent branches not taken by our tests.
func dataReferences(code []byte, pc uint64, add func(uint64)) {
	var registers [32]pageReference

	forget := func(register uint32) {
		ref := registers[register]
		if ref.valid && !ref.resolved {
			for offset := uint64(0); offset < 16*4096; offset += 4096 {
				add(ref.address + offset)
			}
		}

		registers[register] = pageReference{}
	}
	uncertain := func(register uint32) {
		// An earlier understood access does not prove that a surviving
		// address won't be indexed differently in another basic block.
		registers[register].resolved = false

		forget(register)
	}
	flush := func() {
		for register := range registers {
			uncertain(uint32(register))
		}
	}
	access := func(register uint32, offset int64, size uint64) {
		ref := &registers[register]
		if ref.valid {
			address := uint64(int64(ref.address) + offset)
			add(address)
			add(address + size - 1)

			ref.resolved = true
		}
	}

	for off := 0; off+4 <= len(code); off += 4 {
		word := binary.LittleEndian.Uint32(code[off:])
		rd, rn := word&31, word>>5&31

		switch {
		case word&0x9f000000 == 0x90000000: // ADRP
			forget(rd)

			address := adrp(word, pc+uint64(off))
			add(address)
			registers[rd] = pageReference{address: address, valid: rd != 31}
		case word&0x9f000000 == 0x10000000: // ADR (signed, byte-relative)
			forget(rd)

			imm := int64(word>>5&0x7ffff)<<2 | int64(word>>29&3)
			if imm&(1<<20) != 0 {
				imm -= 1 << 21
			}

			address := uint64(int64(pc) + int64(off) + imm)
			add(address)
			registers[rd] = pageReference{address: address, valid: rd != 31, resolved: true}
		case word&0x9f000000 == 0x91000000: // 64-bit ADD/SUB immediate
			ref := registers[rn]
			if ref.valid {
				imm := int64(word >> 10 & 0xfff)
				if word&(1<<22) != 0 {
					imm <<= 12
				}

				if word&(1<<30) != 0 {
					imm = -imm
				}

				access(rn, imm, 1)
				ref.address = uint64(int64(ref.address) + imm)
				ref.resolved = true
			}

			forget(rd)

			registers[rd] = ref
		case word&0x3b000000 == 0x39000000: // unsigned-immediate LDR/STR (including SIMD)
			scale := word >> 30
			if word&(1<<26) != 0 && word&(1<<23) != 0 {
				scale = 4
			}

			access(rn, int64(word>>10&0xfff)<<scale, 1<<scale)

			if word&(1<<26) == 0 && word&0x00c00000 != 0 {
				forget(rd)
			}
		case word&0x3b200000 == 0x38000000: // unscaled/pre/post-indexed load/store
			imm := int64(int32(word<<11) >> 23)

			mode := word >> 10 & 3
			if mode == 1 { // post-index uses the original address
				access(rn, 0, 16)
			} else {
				access(rn, imm, 16)
			}

			if (mode == 1 || mode == 3) && registers[rn].valid {
				registers[rn].address = uint64(int64(registers[rn].address) + imm)
			}

			if word&(1<<26) == 0 && word&0x00c00000 != 0 {
				forget(rd)
			}
		case word&0x3a000000 == 0x28000000: // load/store pair
			scale := uint32(2) + word>>31
			if word&(1<<26) != 0 {
				scale = 2 + word>>30
			}

			imm := int64(int32(word<<10)>>25) << scale

			mode := word >> 23 & 3
			if mode == 1 {
				access(rn, 0, 2<<scale)
			} else {
				access(rn, imm, 2<<scale)
			}

			if (mode == 1 || mode == 3) && registers[rn].valid {
				registers[rn].address = uint64(int64(registers[rn].address) + imm)
			}

			if word&(1<<26) == 0 && word&(1<<22) != 0 {
				forget(rd)
				forget(word >> 10 & 31)
			}
		case word&0x1f800000 == 0x12800000: // MOVN/MOVZ/MOVK immediate
			forget(rd)
		case word == 0xd503201f: // NOP
		case word&0x1c000000 == 0x14000000: // branch, exception, or system instruction
			flush()
		default:
			// Preserve independent registers across intervening instructions.
			// Potential operands of an unknown instruction fall back instead
			// of propagating an address through unrecognized arithmetic.
			uncertain(rn)
			uncertain(word >> 16 & 31)
			forget(rd)
		}
	}

	for register := range registers {
		forget(uint32(register))
	}
}
