//nolint:wsl // Instruction decoding is clearer when related operations stay together.
package machine

import (
	"encoding/binary"
	"fmt"

	"github.com/majd/ipatool/v2/internal/sap/unicorn"
)

const (
	arm64AtomicMask = uint32(0xffe0fc00)
	arm64CASAL32    = uint32(0x88e0fc00)
	arm64CASAL64    = uint32(0xc8e0fc00)
	arm64SWPAL32    = uint32(0xb8e08000)
	arm64SWPAL64    = uint32(0xf8e08000)
	arm64EOR3Mask   = uint32(0xffe08000)
	arm64EOR3       = uint32(0xce000000)
)

type arm64Compatibility struct {
	engine *unicorn.Engine
}

func newARM64Compatibility(engine *unicorn.Engine) *arm64Compatibility {
	return &arm64Compatibility{engine: engine}
}

func (c *arm64Compatibility) emulateInvalidInstruction() (bool, error) {
	pc, err := c.engine.RegRead(unicorn.RegPC)
	if err != nil {
		return false, fmt.Errorf("read arm64 instruction address: %w", err)
	}

	data, err := c.engine.MemRead(pc, 4)
	if err != nil {
		return false, nil
	}

	instruction := binary.LittleEndian.Uint32(data)
	if instruction&arm64EOR3Mask == arm64EOR3 {
		if err := c.emulateEOR3(instruction); err != nil {
			return false, err
		}
		if err := c.engine.RegWrite(unicorn.RegPC, pc+4); err != nil {
			return false, fmt.Errorf("advance arm64 instruction address: %w", err)
		}

		return true, nil
	}

	opcode := instruction & arm64AtomicMask

	var width uint64

	switch opcode {
	case arm64CASAL32, arm64SWPAL32:
		width = 4
	case arm64CASAL64, arm64SWPAL64:
		width = 8
	default:
		return false, nil
	}

	source := (instruction >> 16) & 31
	base := (instruction >> 5) & 31
	destination := instruction & 31

	address, err := c.readBaseRegister(base)
	if err != nil {
		return false, err
	}

	memory, err := c.engine.MemRead(address, width)
	if err != nil {
		return false, fmt.Errorf("read arm64 atomic operand at %#x: %w", address, err)
	}

	oldValue := binary.LittleEndian.Uint64(append(memory, make([]byte, 8-width)...))
	sourceValue, err := c.readValueRegister(source)
	if err != nil {
		return false, err
	}

	if width == 4 {
		sourceValue = uint64(uint32(sourceValue))
	}

	switch opcode {
	case arm64CASAL32, arm64CASAL64:
		newValue, readErr := c.readValueRegister(destination)
		if readErr != nil {
			return false, readErr
		}
		if width == 4 {
			newValue = uint64(uint32(newValue))
		}
		if oldValue == sourceValue {
			if err := c.writeMemoryValue(address, newValue, width); err != nil {
				return false, err
			}
		}
		if err := c.writeValueRegister(source, oldValue); err != nil {
			return false, err
		}
	case arm64SWPAL32, arm64SWPAL64:
		if err := c.writeMemoryValue(address, sourceValue, width); err != nil {
			return false, err
		}
		if err := c.writeValueRegister(destination, oldValue); err != nil {
			return false, err
		}
	}

	if err := c.engine.RegWrite(unicorn.RegPC, pc+4); err != nil {
		return false, fmt.Errorf("advance arm64 instruction address: %w", err)
	}

	return true, nil
}

func (c *arm64Compatibility) emulateEOR3(instruction uint32) error {
	destination := instruction & 31
	first := (instruction >> 5) & 31
	third := (instruction >> 10) & 31
	second := (instruction >> 16) & 31

	firstValue, err := c.engine.RegRead128(unicorn.RegV0 + int(first))
	if err != nil {
		return fmt.Errorf("read arm64 vector register %d: %w", first, err)
	}
	secondValue, err := c.engine.RegRead128(unicorn.RegV0 + int(second))
	if err != nil {
		return fmt.Errorf("read arm64 vector register %d: %w", second, err)
	}
	thirdValue, err := c.engine.RegRead128(unicorn.RegV0 + int(third))
	if err != nil {
		return fmt.Errorf("read arm64 vector register %d: %w", third, err)
	}

	var result [16]byte
	for index := range result {
		result[index] = firstValue[index] ^ secondValue[index] ^ thirdValue[index]
	}
	if err := c.engine.RegWrite128(unicorn.RegV0+int(destination), result); err != nil {
		return fmt.Errorf("write arm64 vector register %d: %w", destination, err)
	}

	return nil
}

func (c *arm64Compatibility) readBaseRegister(index uint32) (uint64, error) {
	register := unicorn.RegSP

	if index != 31 {
		var ok bool
		register, ok = arm64GeneralRegister(index)
		if !ok {
			return 0, fmt.Errorf("invalid arm64 base register %d", index)
		}
	}

	value, err := c.engine.RegRead(register)
	if err != nil {
		return 0, fmt.Errorf("read arm64 base register %d: %w", index, err)
	}

	return value, nil
}

func (c *arm64Compatibility) readValueRegister(index uint32) (uint64, error) {
	if index == 31 {
		return 0, nil
	}

	register, ok := arm64GeneralRegister(index)
	if !ok {
		return 0, fmt.Errorf("invalid arm64 value register %d", index)
	}

	value, err := c.engine.RegRead(register)
	if err != nil {
		return 0, fmt.Errorf("read arm64 value register %d: %w", index, err)
	}

	return value, nil
}

func (c *arm64Compatibility) writeValueRegister(index uint32, value uint64) error {
	if index == 31 {
		return nil
	}

	register, ok := arm64GeneralRegister(index)
	if !ok {
		return fmt.Errorf("invalid arm64 value register %d", index)
	}
	if err := c.engine.RegWrite(register, value); err != nil {
		return fmt.Errorf("write arm64 value register %d: %w", index, err)
	}

	return nil
}

func (c *arm64Compatibility) writeMemoryValue(address, value, width uint64) error {
	var data [8]byte

	binary.LittleEndian.PutUint64(data[:], value)
	if err := c.engine.MemWrite(address, data[:width]); err != nil {
		return fmt.Errorf("write arm64 atomic operand at %#x: %w", address, err)
	}

	return nil
}

func arm64GeneralRegister(index uint32) (int, bool) {
	switch {
	case index <= 28:
		return unicorn.RegX0 + int(index), true
	case index == 29:
		return unicorn.RegX29, true
	case index == 30:
		return unicorn.RegLR, true
	default:
		return 0, false
	}
}
