package machine

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sort"
)

const maxGuestTransfer = uint64(64 << 20)

func (s *shims) registerMemoryServices() error {
	services := []struct {
		names   []string
		handler shimHandler
	}{
		{[]string{"_malloc"}, s.malloc},
		{[]string{"_malloc_good_size"}, s.mallocGoodSize},
		{[]string{"_malloc_size"}, s.mallocSize},
		{[]string{"_calloc"}, s.calloc},
		{[]string{"_realloc", "_reallocf"}, s.realloc},
		{[]string{"_free"}, s.free},
		{[]string{"_memcpy", "_memmove"}, s.memmove},
		{[]string{"_memset"}, s.memset},
		{[]string{"___bzero"}, s.bzero},
		{[]string{"___memcpy_chk"}, s.checkedMemcpy},
		{[]string{"___memset_chk"}, s.checkedMemset},
		{[]string{"_memcmp"}, s.memcmp},
		{[]string{"_strcmp"}, s.strcmp},
		{[]string{"_strncmp"}, s.strncmp},
		{[]string{"_strlen"}, s.strlen},
	}
	for _, service := range services {
		if err := s.addAliases(service.names, service.handler); err != nil {
			return fmt.Errorf("clear allocated guest memory: %w", err)
		}
	}

	return nil
}

func (s *shims) malloc() error {
	size, err := s.argument(0)
	if err != nil {
		return fmt.Errorf("write reallocated guest memory: %w", err)
	}

	address, err := s.allocate(size)
	if err != nil {
		return err
	}

	return s.setResult(address)
}

func (s *shims) mallocGoodSize() error {
	size, err := s.argument(0)
	if err != nil {
		return err
	}

	return s.setResult(align(max(size, 1), 16))
}

func (s *shims) mallocSize() error {
	address, err := s.argument(0)
	if err != nil {
		return err
	}

	allocation, ok := s.allocations[address]
	if !ok {
		return s.setResult(0)
	}

	return s.setResult(allocation.reserved)
}

func (s *shims) calloc() error {
	count, err := s.argument(0)
	if err != nil {
		return err
	}

	size, err := s.argument(1)
	if err != nil {
		return err
	}

	if count != 0 && size > math.MaxUint64/count {
		return errors.New("allocation size overflows")
	}

	total := count * size

	address, err := s.allocate(total)
	if err != nil {
		return err
	}

	if total != 0 {
		if err := s.engine.MemWrite(address, make([]byte, total)); err != nil {
			_ = s.release(address)

			return fmt.Errorf("clear allocated guest memory: %w", err)
		}
	}

	return s.setResult(address)
}

func (s *shims) realloc() error {
	oldAddress, err := s.argument(0)
	if err != nil {
		return fmt.Errorf("write reallocated guest memory: %w", err)
	}

	newSize, err := s.argument(1)
	if err != nil {
		return err
	}

	if oldAddress == 0 {
		address, err := s.allocate(newSize)
		if err != nil {
			return err
		}

		return s.setResult(address)
	}

	oldAllocation, ok := s.allocations[oldAddress]
	if !ok {
		return fmt.Errorf("reallocate unknown pointer %#x", oldAddress)
	}

	if newSize <= oldAllocation.reserved {
		oldAllocation.size = newSize
		s.allocations[oldAddress] = oldAllocation

		return s.setResult(oldAddress)
	}

	newAddress, err := s.allocate(newSize)
	if err != nil {
		return err
	}

	data, err := s.engine.MemRead(oldAddress, oldAllocation.size)
	if err != nil {
		_ = s.release(newAddress)

		return fmt.Errorf("read reallocated guest memory: %w", err)
	}

	if err := s.engine.MemWrite(newAddress, data); err != nil {
		_ = s.release(newAddress)

		return fmt.Errorf("write reallocated guest memory: %w", err)
	}

	if err := s.release(oldAddress); err != nil {
		return err
	}

	return s.setResult(newAddress)
}

func (s *shims) free() error {
	address, err := s.argument(0)
	if err != nil {
		return err
	}

	if address != 0 {
		if err := s.release(address); err != nil {
			return err
		}
	}

	return s.setResult(0)
}

func (s *shims) allocate(size uint64) (uint64, error) {
	if size > maxGuestTransfer {
		return 0, fmt.Errorf("allocation size %d exceeds limit", size)
	}

	reserved := align(max(size, 1), 16)
	for index, block := range s.freeBlocks {
		if block.size < reserved {
			continue
		}

		address := block.address

		if block.size == reserved {
			s.freeBlocks = append(s.freeBlocks[:index], s.freeBlocks[index+1:]...)
		} else {
			s.freeBlocks[index].address += reserved
			s.freeBlocks[index].size -= reserved
		}

		s.allocations[address] = guestAllocation{size: size, reserved: reserved}

		return address, nil
	}

	if s.heapCursor > heapSize || reserved > heapSize-s.heapCursor {
		return 0, errors.New("guest heap exhausted")
	}

	address := heapBase + s.heapCursor
	s.heapCursor += reserved
	s.allocations[address] = guestAllocation{size: size, reserved: reserved}

	return address, nil
}

func (s *shims) release(address uint64) error {
	allocation, ok := s.allocations[address]
	if !ok {
		return fmt.Errorf("free unknown pointer %#x", address)
	}

	if err := s.engine.MemWrite(address, make([]byte, allocation.reserved)); err != nil {
		return fmt.Errorf("clear released guest memory: %w", err)
	}

	delete(s.allocations, address)
	s.freeBlocks = append(s.freeBlocks, freeBlock{address: address, size: allocation.reserved})
	s.coalesceFreeBlocks()

	return nil
}

func (s *shims) coalesceFreeBlocks() {
	sort.Slice(s.freeBlocks, func(left, right int) bool {
		return s.freeBlocks[left].address < s.freeBlocks[right].address
	})

	merged := s.freeBlocks[:0]
	for _, block := range s.freeBlocks {
		last := len(merged) - 1
		if last >= 0 && merged[last].address+merged[last].size == block.address {
			merged[last].size += block.size

			continue
		}

		merged = append(merged, block)
	}

	s.freeBlocks = merged
	for len(s.freeBlocks) != 0 {
		last := len(s.freeBlocks) - 1

		block := s.freeBlocks[last]
		if block.address+block.size != heapBase+s.heapCursor {
			break
		}

		s.heapCursor -= block.size
		s.freeBlocks = s.freeBlocks[:last]
	}
}

func (s *shims) memmove() error {
	destination, err := s.argument(0)
	if err != nil {
		return err
	}

	source, err := s.argument(1)
	if err != nil {
		return err
	}

	length, err := s.argument(2)
	if err != nil {
		return err
	}

	if _, err := checkedSize(length); err != nil {
		return err
	}

	data, err := s.engine.MemRead(source, length)
	if err != nil {
		return fmt.Errorf("read memmove source: %w", err)
	}

	if err := s.engine.MemWrite(destination, data); err != nil {
		return fmt.Errorf("write memmove destination: %w", err)
	}

	return s.setResult(destination)
}

func (s *shims) memset() error {
	destination, err := s.argument(0)
	if err != nil {
		return err
	}

	value, err := s.argument(1)
	if err != nil {
		return err
	}

	length, err := s.argument(2)
	if err != nil {
		return err
	}

	size, err := checkedSize(length)
	if err != nil {
		return err
	}

	if err := s.engine.MemWrite(destination, bytes.Repeat([]byte{byte(value)}, size)); err != nil {
		return fmt.Errorf("write guest memory fill: %w", err)
	}

	return s.setResult(destination)
}

func (s *shims) bzero() error {
	destination, err := s.argument(0)
	if err != nil {
		return err
	}

	length, err := s.argument(1)
	if err != nil {
		return err
	}

	size, err := checkedSize(length)
	if err != nil {
		return err
	}

	if err := s.engine.MemWrite(destination, make([]byte, size)); err != nil {
		return fmt.Errorf("clear guest memory: %w", err)
	}

	return s.setResult(destination)
}

func (s *shims) checkedMemcpy() error {
	length, err := s.argument(2)
	if err != nil {
		return err
	}

	capacity, err := s.argument(3)
	if err != nil {
		return err
	}

	if length > capacity {
		return errors.New("checked copy exceeds destination")
	}

	return s.memmove()
}

func (s *shims) checkedMemset() error {
	length, err := s.argument(2)
	if err != nil {
		return err
	}

	capacity, err := s.argument(3)
	if err != nil {
		return err
	}

	if length > capacity {
		return errors.New("checked fill exceeds destination")
	}

	return s.memset()
}

func (s *shims) memcmp() error {
	left, err := s.argument(0)
	if err != nil {
		return err
	}

	right, err := s.argument(1)
	if err != nil {
		return err
	}

	length, err := s.argument(2)
	if err != nil {
		return err
	}

	if _, err := checkedSize(length); err != nil {
		return err
	}

	a, err := s.engine.MemRead(left, length)
	if err != nil {
		return fmt.Errorf("read left memory operand: %w", err)
	}

	b, err := s.engine.MemRead(right, length)
	if err != nil {
		return fmt.Errorf("read right memory operand: %w", err)
	}

	return s.setResult(uint64(int64(bytes.Compare(a, b))))
}

func (s *shims) strcmp() error {
	left, err := s.argument(0)
	if err != nil {
		return err
	}

	right, err := s.argument(1)
	if err != nil {
		return err
	}

	a, err := s.readCString(left)
	if err != nil {
		return err
	}

	b, err := s.readCString(right)
	if err != nil {
		return err
	}

	return s.setResult(uint64(int64(bytes.Compare([]byte(a), []byte(b)))))
}

func (s *shims) strncmp() error {
	left, err := s.argument(0)
	if err != nil {
		return err
	}

	right, err := s.argument(1)
	if err != nil {
		return err
	}

	length, err := s.argument(2)
	if err != nil {
		return err
	}

	if _, err := checkedSize(length); err != nil {
		return err
	}

	for offset := uint64(0); offset < length; {
		if left > math.MaxUint64-offset || right > math.MaxUint64-offset {
			return errors.New("string comparison address overflows")
		}

		leftAddress := left + offset
		rightAddress := right + offset
		chunk := min(
			length-offset,
			pageSize-leftAddress%pageSize,
			pageSize-rightAddress%pageSize,
		)

		a, err := s.engine.MemRead(leftAddress, chunk)
		if err != nil {
			return fmt.Errorf("read left memory page: %w", err)
		}

		b, err := s.engine.MemRead(rightAddress, chunk)
		if err != nil {
			return fmt.Errorf("read right memory page: %w", err)
		}

		for index := range a {
			if a[index] != b[index] {
				return s.setResult(uint64(int64(int(a[index]) - int(b[index]))))
			}

			if a[index] == 0 {
				return s.setResult(0)
			}
		}

		offset += chunk
	}

	return s.setResult(0)
}

func (s *shims) strlen() error {
	address, err := s.argument(0)
	if err != nil {
		return err
	}

	value, err := s.readCString(address)
	if err != nil {
		return err
	}

	return s.setResult(uint64(len(value)))
}
