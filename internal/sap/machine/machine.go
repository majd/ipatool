package machine

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/sap/machimage"
	"github.com/majd/ipatool/v2/internal/sap/unicorn"
)

const (
	returnAddress = uint64(0x0000000100000000)
	coreFPBase    = uint64(0x0000100000000000)
	commerceBase  = uint64(0x0000100040000000)
	kitBase       = uint64(0x0000100080000000)
	scratchBase   = uint64(0x0000300000000000)
	scratchSize   = uint64(32 << 20)
	heapBase      = uint64(0x0000400000000000)
	heapSize      = uint64(64 << 20)
	stackBase     = uint64(0x0000500000000000)
	stackSize     = uint64(8 << 20)
	stackEnd      = stackBase + stackSize
	pageSize      = uint64(0x1000)
	maxOutputSize = uint64(16 << 20)
)

var coreExportNames = []string{
	"_WIn9UJ86JKdV4dM",
	"_X46O5IeS",
	"_YlCJ3lg",
	"_dku592fbFAj",
	"_fdjkDSAFjklaf2s",
	"_lxpgvVMLd0S7uRl",
}

type entryPoints struct {
	initialize uint64
	exchange   uint64
	sign       uint64
	teardown   uint64
	dispose    uint64
}

type Machine struct {
	engine        *unicorn.Engine
	services      *shims
	entry         entryPoints
	scratchCursor uint64
	closed        bool
}

func Open(ctx context.Context, bundle assets.Bundle) (*Machine, error) {
	if ctx == nil {
		return nil, errors.New("SAP runtime context is nil")
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("open SAP runtime: %w", err)
	}

	coreFP, err := machimage.Open("CoreFP", bundle.CoreFP)
	if err != nil {
		return nil, fmt.Errorf("open CoreFP image: %w", err)
	}

	commerceCore, err := machimage.Open("CommerceCore", bundle.CommerceCore)
	if err != nil {
		return nil, fmt.Errorf("open CommerceCore image: %w", err)
	}

	commerceKit, err := machimage.Open("CommerceKit", bundle.CommerceKit)
	if err != nil {
		return nil, fmt.Errorf("open CommerceKit image: %w", err)
	}

	exports := make(map[string]uint64)
	coreExports := make(map[string]uint64, len(coreExportNames))

	for _, name := range coreExportNames {
		address, err := coreFP.Export(name, coreFPBase)
		if err != nil {
			return nil, fmt.Errorf("resolve CoreFP export %s: %w", name, err)
		}

		exports[name] = address
		coreExports[name] = address
	}

	macAddress, err := commerceCore.Export("_get_mac_address", commerceBase)
	if err != nil {
		return nil, fmt.Errorf("resolve CommerceCore MAC address export: %w", err)
	}

	exports["_get_mac_address"] = macAddress

	entryNames := []string{
		"_cp2g1b9ro",
		"_Mib5yocT",
		"_Fc3vhtJDvr",
		"_IPaI1oem5iL",
		"_jEHf8Xzsv8K",
	}
	resolvedEntries := make(map[string]uint64, len(entryNames))

	for _, name := range entryNames {
		address, err := commerceKit.Export(name, kitBase)
		if err != nil {
			return nil, fmt.Errorf("resolve CommerceKit export %s: %w", name, err)
		}

		exports[name] = address
		resolvedEntries[name] = address
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("open SAP runtime: %w", err)
	}

	engine, err := unicorn.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create Unicorn engine: %w", err)
	}

	machine := &Machine{engine: engine}
	ready := false

	defer func() {
		if !ready {
			_ = machine.Close()
		}
	}()

	for _, region := range []struct {
		address uint64
		size    uint64
	}{
		{returnAddress, pageSize},
		{scratchBase, scratchSize},
		{heapBase, heapSize},
		{stackBase, stackSize},
	} {
		if err := engine.MemMap(region.address, region.size); err != nil {
			return nil, fmt.Errorf("map SAP guest memory at %#x: %w", region.address, err)
		}
	}

	if err := engine.MemWrite(returnAddress, []byte{0xF4}); err != nil {
		return nil, fmt.Errorf("write SAP guest return instruction: %w", err)
	}

	machine.services, err = newShims(engine, coreExports, bundle.CoreFPICXS)
	if err != nil {
		return nil, err
	}

	resolver := func(name string) (uint64, error) {
		if address, ok := exports[name]; ok {
			return address, nil
		}

		return machine.services.resolve(name)
	}
	for _, item := range []struct {
		image *machimage.Image
		base  uint64
	}{
		{coreFP, coreFPBase},
		{commerceCore, commerceBase},
		{commerceKit, kitBase},
	} {
		if err := item.image.Relocate(item.base, resolver); err != nil {
			return nil, fmt.Errorf("relocate SAP guest image: %w", err)
		}

		if err := item.image.Load(engine); err != nil {
			return nil, fmt.Errorf("load SAP guest image: %w", err)
		}
	}

	machine.entry = entryPoints{
		initialize: resolvedEntries["_cp2g1b9ro"],
		exchange:   resolvedEntries["_Mib5yocT"],
		sign:       resolvedEntries["_Fc3vhtJDvr"],
		teardown:   resolvedEntries["_IPaI1oem5iL"],
		dispose:    resolvedEntries["_jEHf8Xzsv8K"],
	}
	ready = true

	return machine, nil
}

func (m *Machine) Initialize(hardwareID []byte) (uint64, error) {
	hardware, err := hardwareBlock(hardwareID)
	if err != nil {
		return 0, err
	}

	m.beginCall()

	defer m.clearScratch()

	contextField, err := m.scratch(nil, 8)
	if err != nil {
		return 0, err
	}

	hardwareAddress, err := m.scratch(hardware, uint64(len(hardware)))
	if err != nil {
		return 0, err
	}

	status, err := m.invoke(m.entry.initialize, contextField, hardwareAddress)
	if err != nil {
		return 0, err
	}

	if int32(status) != 0 {
		return 0, fmt.Errorf("SAP initialization returned %d", int32(status))
	}

	contextValue, err := m.readUint64(contextField)
	if err != nil {
		return 0, err
	}

	if contextValue == 0 {
		return 0, errors.New("SAP initialization returned a null context")
	}

	return contextValue, nil
}

func (m *Machine) Exchange(version uint32, hardwareID []byte, contextValue uint64, input []byte) ([]byte, int32, error) {
	if uint64(len(input)) > math.MaxUint32 {
		return nil, 0, errors.New("SAP exchange input is too large")
	}

	hardware, err := hardwareBlock(hardwareID)
	if err != nil {
		return nil, 0, err
	}

	m.beginCall()

	defer m.clearScratch()

	hardwareAddress, err := m.scratch(hardware, uint64(len(hardware)))
	if err != nil {
		return nil, 0, err
	}

	inputAddress, err := m.scratch(input, uint64(len(input)))
	if err != nil {
		return nil, 0, err
	}

	outputField, err := m.scratch(nil, 8)
	if err != nil {
		return nil, 0, err
	}

	lengthField, err := m.scratch(nil, 8)
	if err != nil {
		return nil, 0, err
	}

	resultField, err := m.scratch(nil, 4)
	if err != nil {
		return nil, 0, err
	}

	status, err := m.invoke(
		m.entry.exchange,
		uint64(version),
		hardwareAddress,
		contextValue,
		inputAddress,
		uint64(len(input)),
		outputField,
		lengthField,
		resultField,
	)
	if err != nil {
		return nil, 0, err
	}

	if int32(status) != 0 {
		return nil, 0, fmt.Errorf("SAP exchange returned %d", int32(status))
	}

	output, err := m.consumeOutput(outputField, lengthField)
	if err != nil {
		return nil, 0, err
	}

	result, err := m.readUint32(resultField)
	if err != nil {
		return nil, 0, err
	}

	return output, int32(result), nil
}

func (m *Machine) Sign(contextValue uint64, input []byte) ([]byte, error) {
	if uint64(len(input)) > math.MaxUint32 {
		return nil, errors.New("SAP signing input is too large")
	}

	m.beginCall()

	defer m.clearScratch()

	inputAddress, err := m.scratch(input, uint64(len(input)))
	if err != nil {
		return nil, err
	}

	outputField, err := m.scratch(nil, 8)
	if err != nil {
		return nil, err
	}

	lengthField, err := m.scratch(nil, 8)
	if err != nil {
		return nil, err
	}

	status, err := m.invoke(
		m.entry.sign,
		contextValue,
		inputAddress,
		uint64(len(input)),
		outputField,
		lengthField,
	)
	if err != nil {
		return nil, err
	}

	if int32(status) != 0 {
		return nil, fmt.Errorf("SAP signing returned %d", int32(status))
	}

	output, err := m.consumeOutput(outputField, lengthField)
	if err != nil {
		return nil, err
	}

	return output, nil
}

func (m *Machine) Teardown(contextValue uint64) error {
	status, err := m.invoke(m.entry.teardown, contextValue)
	if err != nil {
		return err
	}

	if int32(status) != 0 {
		return fmt.Errorf("SAP teardown returned %d", int32(status))
	}

	return nil
}

func (m *Machine) dispose(output uint64) error {
	status, err := m.invoke(m.entry.dispose, output)
	if err != nil {
		return err
	}

	if int32(status) != 0 {
		return fmt.Errorf("SAP storage disposal returned %d", int32(status))
	}

	return nil
}

func (m *Machine) invoke(function uint64, arguments ...uint64) (uint64, error) {
	if m.closed {
		return 0, errors.New("SAP guest machine is closed")
	}

	if function == 0 {
		return 0, errors.New("SAP guest entry point is unavailable")
	}

	registers := [...]int{
		unicorn.RegRDI,
		unicorn.RegRSI,
		unicorn.RegRDX,
		unicorn.RegRCX,
		unicorn.RegR8,
		unicorn.RegR9,
	}
	for index, register := range registers {
		var value uint64
		if index < len(arguments) {
			value = arguments[index]
		}

		if err := m.engine.RegWrite(register, value); err != nil {
			return 0, fmt.Errorf("write SAP guest argument register: %w", err)
		}
	}

	extra := max(len(arguments)-len(registers), 0)

	stackPointer := stackEnd - uint64(extra+1)*8
	if stackPointer%16 != 8 {
		stackPointer -= 8
	}

	if err := m.writeUint64(stackPointer, returnAddress); err != nil {
		return 0, err
	}

	for index := range extra {
		if err := m.writeUint64(stackPointer+8+uint64(index)*8, arguments[len(registers)+index]); err != nil {
			return 0, err
		}
	}

	if err := m.engine.RegWrite(unicorn.RegRSP, stackPointer); err != nil {
		return 0, fmt.Errorf("write SAP guest stack register: %w", err)
	}

	m.services.resetFault()

	if err := m.engine.StartBounded(function, returnAddress, 10*time.Second, 100_000_000); err != nil {
		if m.services.fault != nil {
			return 0, m.services.fault
		}

		return 0, fmt.Errorf("execute SAP guest function: %w", err)
	}

	if m.services.fault != nil {
		return 0, m.services.fault
	}

	instruction, err := m.engine.RegRead(unicorn.RegRIP)
	if err != nil {
		return 0, fmt.Errorf("read SAP guest instruction register: %w", err)
	}

	if instruction != returnAddress {
		return 0, fmt.Errorf("SAP guest stopped unexpectedly at %#x", instruction)
	}

	result, err := m.engine.RegRead(unicorn.RegRAX)
	if err != nil {
		return 0, fmt.Errorf("read SAP guest result register: %w", err)
	}

	return result, nil
}

func (m *Machine) beginCall() {
	m.scratchCursor = 0
}

func (m *Machine) scratch(data []byte, size uint64) (uint64, error) {
	reserved := align(max(size, 1), 16)
	if m.scratchCursor > scratchSize || reserved > scratchSize-m.scratchCursor {
		return 0, errors.New("SAP guest scratch space exhausted")
	}

	address := scratchBase + m.scratchCursor
	m.scratchCursor += reserved

	if len(data) != 0 {
		if uint64(len(data)) > size {
			return 0, errors.New("scratch data exceeds reservation")
		}

		if err := m.engine.MemWrite(address, data); err != nil {
			return 0, fmt.Errorf("write SAP guest scratch data: %w", err)
		}
	} else if size != 0 {
		if err := m.engine.MemWrite(address, make([]byte, size)); err != nil {
			return 0, fmt.Errorf("clear SAP guest scratch data: %w", err)
		}
	}

	return address, nil
}

func (m *Machine) clearScratch() {
	if m.scratchCursor != 0 && m.engine != nil && !m.closed {
		_ = m.engine.MemWrite(scratchBase, make([]byte, m.scratchCursor))
	}

	m.scratchCursor = 0
}

func (m *Machine) consumeOutput(pointerField, lengthField uint64) ([]byte, error) {
	pointer, err := m.readUint64(pointerField)
	if err != nil {
		return nil, err
	}

	length, err := m.readUint64(lengthField)
	if err != nil {
		return nil, err
	}

	var output []byte

	var outputErr error

	switch {
	case length > maxOutputSize:
		outputErr = fmt.Errorf("SAP output is %d bytes, maximum is %d", length, maxOutputSize)
	case length == 0:
	case pointer == 0:
		outputErr = errors.New("SAP returned a null output pointer")
	default:
		output, outputErr = m.engine.MemRead(pointer, length)
	}

	var disposeErr error
	if pointer != 0 {
		disposeErr = m.dispose(pointer)
	}

	return output, errors.Join(outputErr, disposeErr)
}

func (m *Machine) readUint32(address uint64) (uint32, error) {
	data, err := m.engine.MemRead(address, 4)
	if err != nil {
		return 0, fmt.Errorf("read SAP guest uint32: %w", err)
	}

	return binary.LittleEndian.Uint32(data), nil
}

func (m *Machine) readUint64(address uint64) (uint64, error) {
	data, err := m.engine.MemRead(address, 8)
	if err != nil {
		return 0, fmt.Errorf("read SAP guest uint64: %w", err)
	}

	return binary.LittleEndian.Uint64(data), nil
}

func (m *Machine) writeUint64(address, value uint64) error {
	var data [8]byte

	binary.LittleEndian.PutUint64(data[:], value)

	if err := m.engine.MemWrite(address, data[:]); err != nil {
		return fmt.Errorf("write SAP guest uint64: %w", err)
	}

	return nil
}

func (m *Machine) Close() error {
	if m == nil || m.closed {
		return nil
	}

	m.closed = true

	var errs []error

	if m.services != nil {
		errs = append(errs, m.services.close())
	}

	if m.engine != nil {
		errs = append(errs, m.engine.Close())
	}

	return errors.Join(errs...)
}

func hardwareBlock(hardwareID []byte) ([]byte, error) {
	if len(hardwareID) == 0 || len(hardwareID) > 20 {
		return nil, errors.New("hardware ID must contain between 1 and 20 bytes")
	}

	result := make([]byte, 24)
	binary.LittleEndian.PutUint32(result[0:4], uint32(len(hardwareID)))
	copy(result[4:], hardwareID)

	return result, nil
}

func align(value, alignment uint64) uint64 {
	return (value + alignment - 1) &^ (alignment - 1)
}
