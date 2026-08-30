package machine

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/majd/ipatool/v2/internal/sap/unicorn"
)

const (
	shimBase     = uint64(0x0000200000000000)
	shimCodeSize = uint64(0x0000000000080000)
	shimSize     = uint64(0x0000000000100000)
	shimSlotSize = uint64(16)
)

type shimHandler func() error

type shimEntry struct {
	name    string
	handler shimHandler
}

type guestAllocation struct {
	size     uint64
	reserved uint64
}

type freeBlock struct {
	address uint64
	size    uint64
}

type shimOptions struct {
	zeroReturnAliases []string
}

type shims struct {
	engine      *unicorn.Engine
	hook        *unicorn.Hook
	entries     map[uint64]shimEntry
	symbols     map[string]uint64
	codeCursor  uint64
	dataCursor  uint64
	fault       error
	coreExports map[string]uint64
	icxs        []byte
	icxsOffset  int
	errno       uint64
	heapCursor  uint64
	allocations map[uint64]guestAllocation
	freeBlocks  []freeBlock
	iterator    uint32
}

func newShims(engine *unicorn.Engine, coreExports map[string]uint64, icxs []byte) (*shims, error) {
	return newShimsWithOptions(engine, coreExports, icxs, shimOptions{})
}

func newShimsWithOptions(engine *unicorn.Engine, coreExports map[string]uint64, icxs []byte, options shimOptions) (*shims, error) {
	if err := engine.MemMap(shimBase, shimSize); err != nil {
		return nil, fmt.Errorf("map guest service area: %w", err)
	}

	s := &shims{
		engine:      engine,
		entries:     make(map[uint64]shimEntry),
		symbols:     make(map[string]uint64),
		codeCursor:  shimBase,
		dataCursor:  shimBase + shimCodeSize,
		coreExports: coreExports,
		icxs:        icxs,
		allocations: make(map[uint64]guestAllocation),
	}
	if err := s.registerMemoryServices(); err != nil {
		return nil, err
	}

	if err := s.registerPlatformServices(); err != nil {
		return nil, err
	}

	if err := s.addAliases(options.zeroReturnAliases, s.returnZero); err != nil {
		return nil, fmt.Errorf("register profile guest services: %w", err)
	}

	var err error

	s.hook, err = engine.AddCodeHook(shimBase, shimBase+shimCodeSize-1, s.dispatch)
	if err != nil {
		return nil, fmt.Errorf("register guest service dispatcher: %w", err)
	}

	return s, nil
}

func (s *shims) close() error {
	if s.hook == nil {
		return nil
	}

	err := s.hook.Close()
	s.hook = nil

	if err != nil {
		return fmt.Errorf("close guest service hook: %w", err)
	}

	return nil
}

func (s *shims) resolve(name string) (uint64, error) {
	if address, ok := s.symbols[name]; ok {
		return address, nil
	}

	return s.addFunction(name, func() error {
		return fmt.Errorf("guest called unsupported import %s", name)
	})
}

func (s *shims) addAliases(names []string, handler shimHandler) error {
	for _, name := range names {
		if _, err := s.addFunction(name, handler); err != nil {
			return err
		}
	}

	return nil
}

func (s *shims) addFunction(name string, handler shimHandler) (uint64, error) {
	if address, exists := s.symbols[name]; exists {
		return address, nil
	}

	if s.codeCursor+shimSlotSize > shimBase+shimCodeSize {
		return 0, errors.New("guest service code area is full")
	}

	address := s.codeCursor
	s.codeCursor += shimSlotSize

	if err := s.engine.MemWrite(address, []byte{0xC3}); err != nil {
		return 0, fmt.Errorf("write guest service stub: %w", err)
	}

	s.entries[address] = shimEntry{name: name, handler: handler}
	s.symbols[name] = address

	return address, nil
}

func (s *shims) addData(name string, data []byte) (uint64, error) {
	if address, exists := s.symbols[name]; exists {
		return address, nil
	}

	s.dataCursor = align(s.dataCursor, 8)
	if s.dataCursor+uint64(len(data)) > shimBase+shimSize {
		return 0, errors.New("guest service data area is full")
	}

	address := s.dataCursor
	s.dataCursor += uint64(max(len(data), 8))

	if err := s.engine.MemWrite(address, data); err != nil {
		return 0, fmt.Errorf("write guest service data: %w", err)
	}

	s.symbols[name] = address

	return address, nil
}

func (s *shims) dispatch(address uint64, _ uint32) {
	entry, ok := s.entries[address]
	if !ok {
		s.fail(fmt.Errorf("guest entered unknown service address %#x", address))

		return
	}

	if err := entry.handler(); err != nil {
		s.fail(fmt.Errorf("%s: %w", entry.name, err))
	}
}

func (s *shims) fail(err error) {
	if s.fault == nil {
		s.fault = err
	}

	if stopErr := s.engine.Stop(); stopErr != nil && s.fault == nil {
		s.fault = stopErr
	}
}

func (s *shims) resetFault() {
	s.fault = nil
}

func (s *shims) argument(index int) (uint64, error) {
	registers := [...]int{
		unicorn.RegRDI,
		unicorn.RegRSI,
		unicorn.RegRDX,
		unicorn.RegRCX,
		unicorn.RegR8,
		unicorn.RegR9,
	}
	if index >= 0 && index < len(registers) {
		value, err := s.engine.RegRead(registers[index])
		if err != nil {
			return 0, fmt.Errorf("read guest argument register: %w", err)
		}

		return value, nil
	}

	if index < 0 {
		return 0, errors.New("negative guest argument index")
	}

	stack, err := s.engine.RegRead(unicorn.RegRSP)
	if err != nil {
		return 0, fmt.Errorf("read guest stack register: %w", err)
	}

	data, err := s.engine.MemRead(stack+8+uint64(index-len(registers))*8, 8)
	if err != nil {
		return 0, fmt.Errorf("read guest stack argument: %w", err)
	}

	return binary.LittleEndian.Uint64(data), nil
}

func (s *shims) setResult(value uint64) error {
	if err := s.engine.RegWrite(unicorn.RegRAX, value); err != nil {
		return fmt.Errorf("write guest result register: %w", err)
	}

	return nil
}

func (s *shims) readUint32(address uint64) (uint32, error) {
	data, err := s.engine.MemRead(address, 4)
	if err != nil {
		return 0, fmt.Errorf("read guest uint32: %w", err)
	}

	return binary.LittleEndian.Uint32(data), nil
}

func (s *shims) writeUint32(address uint64, value uint32) error {
	var data [4]byte

	binary.LittleEndian.PutUint32(data[:], value)

	if err := s.engine.MemWrite(address, data[:]); err != nil {
		return fmt.Errorf("write guest uint32: %w", err)
	}

	return nil
}

func (s *shims) readUint64(address uint64) (uint64, error) {
	data, err := s.engine.MemRead(address, 8)
	if err != nil {
		return 0, fmt.Errorf("read guest uint64: %w", err)
	}

	return binary.LittleEndian.Uint64(data), nil
}

func (s *shims) writeUint64(address, value uint64) error {
	var data [8]byte

	binary.LittleEndian.PutUint64(data[:], value)

	if err := s.engine.MemWrite(address, data[:]); err != nil {
		return fmt.Errorf("write guest uint64: %w", err)
	}

	return nil
}

func (s *shims) readCString(address uint64) (string, error) {
	const maximum = 4096

	value := make([]byte, 0, 64)
	for len(value) < maximum {
		item, err := s.engine.MemRead(address+uint64(len(value)), 1)
		if err != nil {
			return "", fmt.Errorf("read guest string: %w", err)
		}

		if item[0] == 0 {
			return string(value), nil
		}

		value = append(value, item[0])
	}

	return "", fmt.Errorf("guest string exceeds %d bytes", maximum)
}

func checkedSize(value uint64) (int, error) {
	if value > uint64(math.MaxInt) || value > maxGuestTransfer {
		return 0, fmt.Errorf("guest transfer size %d exceeds limit", value)
	}

	return int(value), nil
}
