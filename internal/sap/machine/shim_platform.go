package machine

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/majd/ipatool/v2/internal/sap/unicorn"
)

const (
	fakeHandle   = uint64(math.MaxUint64)
	coreFPFile   = uint64(3)
	coreFPPath   = "/System/Library/PrivateFrameworks/CoreFP.framework/CoreFP"
	icxsPath     = "./../CoreFP.icxs"
	keySerial    = "IOPlatformSerialNumber"
	keyUUID      = "IOPlatformUUID"
	keyBoard     = "board-id"
	keyedMessage = "objectForKey:"
)

func (s *shims) registerPlatformServices() error {
	services := []struct {
		names   []string
		handler shimHandler
	}{
		{[]string{"_CFBundleGetMainBundle", "_CFDataGetBytePtr", "_CFDataGetLength", "_CFStringGetLength", "_CFStringGetMaximumSizeForEncoding", "_CFUUIDCreateString", "_IORegistryEntryFromPath", "_IORegistryEntrySearchCFProperty", "_IOServiceMatching", "_getenv", "_pthread_self"}, s.returnZero},
		{[]string{"_CFDictionaryGetValue", "_DADiskCopyDescription", "_DADiskCreateFromBSDName", "_DASessionCreate", "_IORegistryEntryCreateCFProperty"}, s.returnFakeHandle},
		{[]string{"_CFRelease", "_IOObjectRelease", "_close", "_close$UNIX2003", "_pthread_mutex_lock", "_pthread_mutex_unlock", "_pthread_rwlock_init", "_pthread_rwlock_init$UNIX2003", "_pthread_rwlock_unlock", "_pthread_rwlock_unlock$UNIX2003", "_pthread_rwlock_wrlock", "_pthread_rwlock_wrlock$UNIX2003"}, s.returnZero},
		{[]string{"_CFStringCreateWithCString"}, s.cfStringCreate},
		{[]string{"_CFStringCreateWithCStringNoCopy"}, s.returnZero},
		{[]string{"_CFStringGetCString"}, s.cfStringGetCString},
		{[]string{"_IOIteratorNext"}, s.ioIteratorNext},
		{[]string{"_IORegistryEntryGetParentEntry"}, s.ioRegistryEntryGetParentEntry},
		{[]string{"_IOServiceGetMatchingServices"}, s.ioServiceGetMatchingServices},
		{[]string{"_IOServiceGetMatchingService"}, s.returnUint32Max},
		{[]string{"_OSAtomicCompareAndSwap32Barrier"}, s.compareAndSwap32},
		{[]string{"___error"}, s.errorPointer},
		{[]string{"_abort", "___stack_chk_fail", "dyld_stub_binder"}, s.abort},
		{[]string{"_arc4random"}, s.arc4random},
		{[]string{"_dlopen"}, s.dlopen},
		{[]string{"_dlsym"}, s.dlsym},
		{[]string{"_fcntl", "_fcntl$UNIX2003", "_lstat$INODE64", "_statfs", "_statfs$INODE64"}, s.returnMinusOne},
		{[]string{"_gettimeofday"}, s.gettimeofday},
		{[]string{"_objc_msgSend"}, s.objcMsgSend},
		{[]string{"_open", "_open$UNIX2003"}, s.open},
		{[]string{"_pthread_once"}, s.pthreadOnce},
		{[]string{"_read", "_read$UNIX2003"}, s.read},
		{[]string{"_sysctl"}, s.returnMinusOne},
		{[]string{"_sysctlbyname"}, s.sysctlbyname},
	}
	for _, service := range services {
		if err := s.addAliases(service.names, service.handler); err != nil {
			return err
		}
	}

	var err error

	s.errno, err = s.addData("guest.errno", make([]byte, 8))
	if err != nil {
		return err
	}

	if _, err := s.addData("___stack_chk_guard", []byte{0xA5, 0x71, 0x3C, 0xD9, 0x86, 0x42, 0xEF, 0x10}); err != nil {
		return err
	}

	for _, name := range []string{
		"_kCFAllocatorDefault",
		"_kCFAllocatorNull",
		"_kDADiskDescriptionVolumeUUIDKey",
		"_kIOMasterPortDefault",
	} {
		if _, err := s.addData(name, make([]byte, 8)); err != nil {
			return err
		}
	}

	return nil
}

func (s *shims) returnZero() error {
	return s.setResult(0)
}

func (s *shims) returnFakeHandle() error {
	return s.setResult(fakeHandle)
}

func (s *shims) returnUint32Max() error {
	return s.setResult(math.MaxUint32)
}

func (s *shims) returnMinusOne() error {
	return s.setResult(math.MaxUint64)
}

func (s *shims) cfStringCreate() error {
	address, err := s.argument(1)
	if err != nil {
		return err
	}

	value, err := s.readCString(address)
	if err != nil {
		return err
	}

	switch value {
	case keySerial, keyUUID, keyBoard:
		return s.setResult(fakeHandle)
	default:
		return s.setResult(0)
	}
}

func (s *shims) cfStringGetCString() error {
	buffer, err := s.argument(1)
	if err != nil {
		return err
	}

	capacity, err := s.argument(2)
	if err != nil {
		return err
	}

	if buffer == 0 || capacity == 0 {
		return s.setResult(0)
	}

	if err := s.engine.MemWrite(buffer, []byte{0}); err != nil {
		return fmt.Errorf("terminate guest string buffer: %w", err)
	}

	return s.setResult(1)
}

func (s *shims) ioIteratorNext() error {
	s.iterator++

	return s.setResult(uint64(s.iterator % 2))
}

func (s *shims) ioRegistryEntryGetParentEntry() error {
	parent, err := s.argument(2)
	if err != nil {
		return err
	}

	if parent == 0 {
		return errors.New("parent registry entry output is null")
	}

	if err := s.writeUint32(parent, math.MaxUint32); err != nil {
		return err
	}

	return s.setResult(0)
}

func (s *shims) ioServiceGetMatchingServices() error {
	iterator, err := s.argument(2)
	if err != nil {
		return err
	}

	if iterator == 0 {
		return errors.New("matching services iterator output is null")
	}

	s.iterator = 0
	if err := s.writeUint32(iterator, math.MaxUint32); err != nil {
		return err
	}

	return s.setResult(0)
}

func (s *shims) compareAndSwap32() error {
	oldValue, err := s.argument(0)
	if err != nil {
		return err
	}

	newValue, err := s.argument(1)
	if err != nil {
		return err
	}

	address, err := s.argument(2)
	if err != nil {
		return err
	}

	current, err := s.readUint32(address)
	if err != nil {
		return err
	}

	if current != uint32(oldValue) {
		return s.setResult(0)
	}

	if err := s.writeUint32(address, uint32(newValue)); err != nil {
		return err
	}

	return s.setResult(1)
}

func (s *shims) errorPointer() error {
	return s.setResult(s.errno)
}

func (s *shims) abort() error {
	return errors.New("guest aborted")
}

func (s *shims) arc4random() error {
	var value [4]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Errorf("generate guest random value: %w", err)
	}

	return s.setResult(uint64(binary.LittleEndian.Uint32(value[:])))
}

func (s *shims) dlopen() error {
	pathAddress, err := s.argument(0)
	if err != nil {
		return err
	}

	path, err := s.readCString(pathAddress)
	if err != nil {
		return err
	}

	if path == coreFPPath {
		return s.setResult(fakeHandle)
	}

	return s.setResult(0)
}

func (s *shims) dlsym() error {
	nameAddress, err := s.argument(1)
	if err != nil {
		return err
	}

	name, err := s.readCString(nameAddress)
	if err != nil {
		return err
	}

	address := s.coreExports["_"+name]

	return s.setResult(address)
}

func (s *shims) gettimeofday() error {
	timeAddress, err := s.argument(0)
	if err != nil {
		return err
	}

	zoneAddress, err := s.argument(1)
	if err != nil {
		return err
	}

	now := time.Now()

	if timeAddress != 0 {
		var value [16]byte

		binary.LittleEndian.PutUint64(value[0:8], uint64(now.Unix()))
		binary.LittleEndian.PutUint32(value[8:12], uint32(now.Nanosecond()/1000))

		if err := s.engine.MemWrite(timeAddress, value[:]); err != nil {
			return fmt.Errorf("write guest time value: %w", err)
		}
	}

	if zoneAddress != 0 {
		if err := s.engine.MemWrite(zoneAddress, make([]byte, 8)); err != nil {
			return fmt.Errorf("clear guest time zone: %w", err)
		}
	}

	return s.setResult(0)
}

func (s *shims) objcMsgSend() error {
	selectorAddress, err := s.argument(1)
	if err != nil {
		return err
	}

	selector, err := s.readCString(selectorAddress)
	if err != nil {
		return err
	}

	if selector == keyedMessage {
		return s.setResult(fakeHandle)
	}

	return s.setResult(0)
}

func (s *shims) open() error {
	pathAddress, err := s.argument(0)
	if err != nil {
		return err
	}

	path, err := s.readCString(pathAddress)
	if err != nil {
		return err
	}

	if path == icxsPath {
		s.icxsOffset = 0

		return s.setResult(coreFPFile)
	}

	return s.returnMinusOne()
}

func (s *shims) pthreadOnce() error {
	control, err := s.argument(0)
	if err != nil {
		return err
	}

	initializer, err := s.argument(1)
	if err != nil {
		return err
	}

	value, err := s.readUint64(control)
	if err != nil {
		return err
	}

	if value == 0 {
		return s.setResult(0)
	}

	if err := s.writeUint64(control, 0); err != nil {
		return err
	}

	stack, err := s.engine.RegRead(unicorn.RegRSP)
	if err != nil {
		return fmt.Errorf("read guest stack register: %w", err)
	}

	stack -= 8
	if err := s.writeUint64(stack, initializer); err != nil {
		return err
	}

	if err := s.engine.RegWrite(unicorn.RegRSP, stack); err != nil {
		return fmt.Errorf("write guest stack register: %w", err)
	}

	return s.setResult(0)
}

func (s *shims) read() error {
	descriptor, err := s.argument(0)
	if err != nil {
		return err
	}

	buffer, err := s.argument(1)
	if err != nil {
		return err
	}

	requested, err := s.argument(2)
	if err != nil {
		return err
	}

	if descriptor != coreFPFile {
		return s.returnMinusOne()
	}

	size, err := checkedSize(requested)
	if err != nil {
		return err
	}

	remaining := len(s.icxs) - s.icxsOffset
	if size > remaining {
		size = remaining
	}

	if size != 0 {
		if err := s.engine.MemWrite(buffer, s.icxs[s.icxsOffset:s.icxsOffset+size]); err != nil {
			return fmt.Errorf("write guest ICXS data: %w", err)
		}

		s.icxsOffset += size
	}

	return s.setResult(uint64(size))
}

func (s *shims) sysctlbyname() error {
	lengthAddress, err := s.argument(2)
	if err != nil {
		return err
	}

	if lengthAddress != 0 {
		if err := s.writeUint64(lengthAddress, 0); err != nil {
			return err
		}
	}

	return s.setResult(0)
}
