package machine

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/sap/unicorn"
)

func TestOpenHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Open(ctx, assets.Bundle{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open error = %v, want %v", err, context.Canceled)
	}
}

func TestHardwareBlock(t *testing.T) {
	identifier := []byte{1, 2, 3, 4, 5, 6}

	block, err := hardwareBlock(identifier)
	if err != nil {
		t.Fatal(err)
	}

	if len(block) != 24 || binary.LittleEndian.Uint32(block[:4]) != uint32(len(identifier)) {
		t.Fatalf("unexpected hardware block header: %x", block)
	}

	if string(block[4:10]) != string(identifier) {
		t.Fatalf("hardware ID = %x", block[4:10])
	}

	if _, err := hardwareBlock(make([]byte, 21)); err == nil {
		t.Fatal("oversized hardware ID was accepted")
	}
}

func TestGuestServiceDispatchAndStackArguments(t *testing.T) {
	machine := newServiceTestMachine(t)

	address, err := machine.services.addFunction("test.stackArguments", func() error {
		seventh, err := machine.services.argument(6)
		if err != nil {
			return err
		}

		eighth, err := machine.services.argument(7)
		if err != nil {
			return err
		}

		return machine.services.setResult(seventh + eighth)
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := machine.invoke(address, 1, 2, 3, 4, 5, 6, 7, 8)
	if err != nil {
		t.Fatal(err)
	}

	if result != 15 {
		t.Fatalf("result = %d, want 15", result)
	}
}

func TestUnknownGuestServiceFailsClosed(t *testing.T) {
	machine := newServiceTestMachine(t)

	address, err := machine.services.resolve("test.unsupported")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := machine.invoke(address); err == nil || !strings.Contains(err.Error(), "unsupported import") {
		t.Fatalf("invoke error = %v", err)
	}
}

func TestGuestAllocatorReusesAndClearsFreedMemory(t *testing.T) {
	machine := newServiceTestMachine(t)

	address, err := machine.services.allocate(64)
	if err != nil {
		t.Fatal(err)
	}

	if err := machine.engine.MemWrite(address, bytes.Repeat([]byte{0xa5}, 64)); err != nil {
		t.Fatal(err)
	}

	if err := machine.services.release(address); err != nil {
		t.Fatal(err)
	}

	reused, err := machine.services.allocate(32)
	if err != nil {
		t.Fatal(err)
	}

	if reused != address {
		t.Fatalf("reused address = %#x, want %#x", reused, address)
	}

	data, err := machine.engine.MemRead(reused, 32)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, make([]byte, 32)) {
		t.Fatalf("reused allocation was not cleared: %x", data)
	}

	if err := machine.services.release(reused); err != nil {
		t.Fatal(err)
	}

	if machine.services.heapCursor != 0 || len(machine.services.freeBlocks) != 0 {
		t.Fatalf("released heap was not reclaimed: cursor=%d free=%v", machine.services.heapCursor, machine.services.freeBlocks)
	}
}

func TestMallocSizeReturnsUsableSize(t *testing.T) {
	machine := newServiceTestMachine(t)

	address, err := machine.services.allocate(1)
	if err != nil {
		t.Fatal(err)
	}

	result, err := machine.invoke(machine.services.symbols["_malloc_size"], address)
	if err != nil {
		t.Fatal(err)
	}

	if result != 16 {
		t.Fatalf("malloc_size = %d, want 16", result)
	}
}

func TestStrncmpStopsAtMappedPageBoundary(t *testing.T) {
	machine := newServiceTestMachine(t)
	left := returnAddress + pageSize - 2
	right := stackBase + pageSize - 2

	for _, address := range []uint64{left, right} {
		if err := machine.engine.MemWrite(address, []byte{'a', 0}); err != nil {
			t.Fatal(err)
		}
	}

	result, err := machine.invoke(machine.services.symbols["_strncmp"], left, right, 64)
	if err != nil {
		t.Fatal(err)
	}

	if result != 0 {
		t.Fatalf("strncmp = %d, want 0", result)
	}
}

func TestPlatformServicesInitializeOutputs(t *testing.T) {
	machine := newServiceTestMachine(t)
	buffer := stackBase + pageSize

	if err := machine.engine.MemWrite(buffer, bytes.Repeat([]byte{0xa5}, 16)); err != nil {
		t.Fatal(err)
	}

	result, err := machine.invoke(machine.services.symbols["_CFStringGetCString"], fakeHandle, buffer, 16, 0)
	if err != nil {
		t.Fatal(err)
	}

	if result != 1 {
		t.Fatalf("CFStringGetCString = %d, want 1", result)
	}

	data, err := machine.engine.MemRead(buffer, 1)
	if err != nil {
		t.Fatal(err)
	}

	if data[0] != 0 {
		t.Fatalf("CFStringGetCString output = %#x, want NUL", data[0])
	}

	for _, name := range []string{"_IORegistryEntryGetParentEntry", "_IOServiceGetMatchingServices"} {
		if err := machine.engine.MemWrite(buffer, bytes.Repeat([]byte{0xa5}, 4)); err != nil {
			t.Fatal(err)
		}

		result, err := machine.invoke(machine.services.symbols[name], 0, 0, buffer)
		if err != nil {
			t.Fatal(err)
		}

		if result != 0 {
			t.Fatalf("%s = %d, want 0", name, result)
		}

		data, err := machine.engine.MemRead(buffer, 4)
		if err != nil {
			t.Fatal(err)
		}

		if binary.LittleEndian.Uint32(data) != math.MaxUint32 {
			t.Fatalf("%s output = %x, want uint32 max", name, data)
		}
	}

	result, err = machine.invoke(machine.services.symbols["_statfs"], 0, buffer)
	if err != nil {
		t.Fatal(err)
	}

	if result != math.MaxUint64 {
		t.Fatalf("statfs = %#x, want -1", result)
	}
}

func TestConsumeOutputDisposesAllocation(t *testing.T) {
	machine := newServiceTestMachine(t)

	output, err := machine.services.allocate(4)
	if err != nil {
		t.Fatal(err)
	}

	if err := machine.engine.MemWrite(output, []byte("test")); err != nil {
		t.Fatal(err)
	}

	pointerField := scratchBase
	lengthField := scratchBase + 8

	if err := machine.writeUint64(pointerField, output); err != nil {
		t.Fatal(err)
	}

	if err := machine.writeUint64(lengthField, 4); err != nil {
		t.Fatal(err)
	}

	machine.entry.dispose, err = machine.services.addFunction("test.dispose", machine.services.free)
	if err != nil {
		t.Fatal(err)
	}

	data, err := machine.consumeOutput(pointerField, lengthField)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != "test" {
		t.Fatalf("output = %q, want test", data)
	}

	if _, ok := machine.services.allocations[output]; ok {
		t.Fatal("output allocation was not released")
	}
}

func newServiceTestMachine(t *testing.T) *Machine {
	t.Helper()

	engine, err := unicorn.New(context.Background())
	if err != nil {
		t.Fatalf("create Unicorn engine: %v", err)
	}

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
			_ = engine.Close()

			t.Fatal(err)
		}
	}

	if err := engine.MemWrite(returnAddress, []byte{0xF4}); err != nil {
		_ = engine.Close()

		t.Fatal(err)
	}

	services, err := newShims(engine, map[string]uint64{}, nil)
	if err != nil {
		_ = engine.Close()

		t.Fatal(err)
	}

	machine := &Machine{engine: engine, services: services}

	t.Cleanup(func() {
		if err := machine.Close(); err != nil {
			t.Errorf("close machine: %v", err)
		}
	})

	return machine
}
