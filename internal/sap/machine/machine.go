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

const sapGuestTimeout = time.Minute

const (
	returnAddress = uint64(0x0000000100000000)
	coreFPBase    = uint64(0x0000100000000000)
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
	arm64         *arm64Compatibility
	services      *shims
	entry         entryPoints
	scratchCursor uint64
	closed        bool
}

type imageSpec struct {
	name      string
	data      []byte
	base      uint64
	prelinked map[uint64]string
	slots     map[uint64]string
	functions map[uint64]string
}

type runtimeOptions struct {
	extraImages []imageSpec
	shims       shimOptions
}

func Open(ctx context.Context, bundle assets.Bundle) (*Machine, error) {
	machine, exports, err := openRuntime(ctx, bundle, runtimeOptions{})
	if err != nil {
		return nil, err
	}

	machine.entry = entryPoints{
		initialize: exports["_cp2g1b9ro"],
		exchange:   exports["_Mib5yocT"],
		sign:       exports["_Fc3vhtJDvr"],
		teardown:   exports["_IPaI1oem5iL"],
		dispose:    exports["_jEHf8Xzsv8K"],
	}

	return machine, nil
}

func openRuntime(ctx context.Context, bundle assets.Bundle, options runtimeOptions) (*Machine, map[string]uint64, error) {
	if ctx == nil {
		return nil, nil, errors.New("SAP runtime context is nil")
	}

	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("open SAP runtime: %w", err)
	}

	imageSpecs := make([]imageSpec, 0, 2+len(options.extraImages))
	imageSpecs = append(imageSpecs,
		imageSpec{name: "CoreFP", data: bundle.CoreFP, base: coreFPBase},
		imageSpec{
			name:      "AppleMediaServices",
			data:      bundle.AppleMediaServices,
			prelinked: macOS27SharedCacheFunctions,
			slots:     macOS27SharedCacheSlots,
			functions: macOS27SharedCacheFunctions,
		},
	)
	imageSpecs = append(imageSpecs, options.extraImages...)

	images := make(map[string]*machimage.Image, len(imageSpecs))

	for _, spec := range imageSpecs {
		image, err := machimage.Open(spec.name, spec.data)
		if err != nil {
			return nil, nil, fmt.Errorf("open %s image: %w", spec.name, err)
		}

		images[spec.name] = image
	}

	exports := make(map[string]uint64)
	coreExports := make(map[string]uint64, len(coreExportNames))
	coreFP := images["CoreFP"]

	for _, name := range coreExportNames {
		address, err := coreFP.Export(name, coreFPBase)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve CoreFP export %s: %w", name, err)
		}

		exports[name] = address
		coreExports[name] = address
	}

	appleMediaServices := images["AppleMediaServices"]
	for _, name := range []string{
		"_cp2g1b9ro",
		"_Mib5yocT",
		"_Fc3vhtJDvr",
		"_IPaI1oem5iL",
		"_jEHf8Xzsv8K",
	} {
		address, err := appleMediaServices.Export(name, appleMediaServices.Base())
		if err != nil {
			return nil, nil, fmt.Errorf("resolve AppleMediaServices export %s: %w", name, err)
		}

		exports[name] = address
	}

	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("open SAP runtime: %w", err)
	}

	engine, err := unicorn.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("create Unicorn engine: %w", err)
	}

	machine := &Machine{engine: engine, arm64: newARM64Compatibility(engine)}
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
			return nil, nil, fmt.Errorf("map SAP guest memory at %#x: %w", region.address, err)
		}
	}

	if err := engine.MemWrite(returnAddress, []byte{0x00, 0x00, 0x20, 0xd4}); err != nil {
		return nil, nil, fmt.Errorf("write SAP guest return instruction: %w", err)
	}

	machine.services, err = newShimsWithOptions(engine, coreExports, bundle.CoreFPICXS, options.shims)
	if err != nil {
		return nil, nil, err
	}

	resolver := func(name string) (uint64, error) {
		if address, ok := exports[name]; ok {
			return address, nil
		}

		return machine.services.resolve(name)
	}

	for _, spec := range imageSpecs {
		if err := loadPrelinkedSlots(engine, spec.slots, resolver); err != nil {
			return nil, nil, fmt.Errorf("load %s shared-cache slots: %w", spec.name, err)
		}

		if err := loadPrelinkedFunctions(engine, spec.functions, resolver); err != nil {
			return nil, nil, fmt.Errorf("load %s shared-cache functions: %w", spec.name, err)
		}
	}

	for _, spec := range imageSpecs {
		image := images[spec.name]

		var err error

		if spec.prelinked != nil {
			err = image.PreparePrelinked(spec.prelinked, resolver)
		} else {
			err = image.Relocate(spec.base, resolver)
		}

		if err != nil {
			return nil, nil, fmt.Errorf("relocate SAP guest image: %w", err)
		}

		if err := image.Load(engine); err != nil {
			return nil, nil, fmt.Errorf("load SAP guest image: %w", err)
		}
	}

	ready = true

	return machine, exports, nil
}

func loadPrelinkedFunctions(engine *unicorn.Engine, functions map[uint64]string, resolve func(string) (uint64, error)) error {
	pages := make(map[uint64]struct{})
	for address := range functions {
		pages[address-address%pageSize] = struct{}{}
		end := address + 15
		pages[end-end%pageSize] = struct{}{}
	}

	for address := range pages {
		if err := engine.MemMap(address, pageSize); err != nil {
			return fmt.Errorf("map function page at %#x: %w", address, err)
		}
	}

	for address, name := range functions {
		target, err := resolve(name)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", name, err)
		}

		trampoline := make([]byte, 16)
		binary.LittleEndian.PutUint32(trampoline[0:4], 0x58000050) // ldr x16, #8
		binary.LittleEndian.PutUint32(trampoline[4:8], 0xd61f0200) // br x16
		binary.LittleEndian.PutUint64(trampoline[8:16], target)

		if err := engine.MemWrite(address, trampoline); err != nil {
			return fmt.Errorf("write %s trampoline at %#x: %w", name, address, err)
		}
	}

	return nil
}

func loadPrelinkedSlots(engine *unicorn.Engine, slots map[uint64]string, resolve func(string) (uint64, error)) error {
	pages := make(map[uint64]struct{})
	for address := range slots {
		pages[address-address%pageSize] = struct{}{}
	}

	for address := range pages {
		if err := engine.MemMap(address, pageSize); err != nil {
			return fmt.Errorf("map slot page at %#x: %w", address, err)
		}
	}

	for address, name := range slots {
		value, err := resolve(name)
		if err != nil {
			return fmt.Errorf("resolve %s: %w", name, err)
		}

		var data [8]byte

		binary.LittleEndian.PutUint64(data[:], value)

		if err := engine.MemWrite(address, data[:]); err != nil {
			return fmt.Errorf("write %s slot at %#x: %w", name, address, err)
		}
	}

	return nil
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

//nolint:wsl // Register setup and compatibility retries are clearer in execution order.
func (m *Machine) invoke(function uint64, arguments ...uint64) (uint64, error) {
	if m.closed {
		return 0, errors.New("SAP guest machine is closed")
	}

	if function == 0 {
		return 0, errors.New("SAP guest entry point is unavailable")
	}

	registers := [...]int{
		unicorn.RegX0,
		unicorn.RegX1,
		unicorn.RegX2,
		unicorn.RegX3,
		unicorn.RegX4,
		unicorn.RegX5,
		unicorn.RegX6,
		unicorn.RegX7,
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

	stackPointer := stackEnd - uint64(extra)*8
	stackPointer -= stackPointer % 16

	for index := range extra {
		if err := m.writeUint64(stackPointer+uint64(index)*8, arguments[len(registers)+index]); err != nil {
			return 0, err
		}
	}

	if err := m.engine.RegWrite(unicorn.RegSP, stackPointer); err != nil {
		return 0, fmt.Errorf("write SAP guest stack register: %w", err)
	}
	if err := m.engine.RegWrite(unicorn.RegLR, returnAddress); err != nil {
		return 0, fmt.Errorf("write SAP guest return register: %w", err)
	}

	m.services.resetFault()

	// SAP's cryptographic routines have input- and host-dependent instruction
	// counts. Bound execution by wall time without rejecting legitimate work on
	// slower machines for crossing a fixed instruction limit.
	next := function

	for {
		executionErr := m.engine.StartBounded(next, returnAddress, sapGuestTimeout, 0)
		if executionErr == nil {
			break
		}
		if m.services.fault != nil {
			return 0, m.services.fault
		}

		if m.arm64 != nil {
			handled, compatibilityErr := m.arm64.emulateInvalidInstruction()
			if compatibilityErr != nil {
				return 0, compatibilityErr
			}
			if handled {
				next, compatibilityErr = m.engine.RegRead(unicorn.RegPC)
				if compatibilityErr != nil {
					return 0, fmt.Errorf("resume SAP guest function: %w", compatibilityErr)
				}

				continue
			}
		}

		instruction, readErr := m.engine.RegRead(unicorn.RegPC)
		if readErr != nil {
			return 0, fmt.Errorf("execute SAP guest function: %w", executionErr)
		}
		link, linkErr := m.engine.RegRead(unicorn.RegLR)
		if linkErr != nil {
			return 0, fmt.Errorf("execute SAP guest function at %#x: %w", instruction, executionErr)
		}

		return 0, fmt.Errorf("execute SAP guest function at %#x from %#x: %w", instruction, link, executionErr)
	}

	if m.services.fault != nil {
		return 0, m.services.fault
	}

	instruction, err := m.engine.RegRead(unicorn.RegPC)
	if err != nil {
		return 0, fmt.Errorf("read SAP guest instruction register: %w", err)
	}

	if instruction != returnAddress {
		return 0, fmt.Errorf("SAP guest stopped unexpectedly at %#x", instruction)
	}

	result, err := m.engine.RegRead(unicorn.RegX0)
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
