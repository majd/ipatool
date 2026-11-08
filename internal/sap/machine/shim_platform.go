//nolint:nlreturn,wsl // Platform shims mirror guest ABI operations in strict order.
package machine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/majd/ipatool/v2/internal/sap/unicorn"
)

const (
	fakeHandle        = uint64(math.MaxUint64)
	coreFPFile        = uint64(3)
	adiFile           = uint64(5)
	coreFPPath        = "/System/Library/PrivateFrameworks/CoreFP.framework/CoreFP"
	icxsPath          = "./../CoreFP.icxs"
	keySerial         = "IOPlatformSerialNumber"
	keyUUID           = "IOPlatformUUID"
	keyBoard          = "board-id"
	keyedMessage      = "objectForKey:"
	xpcDictionaryType = uint64(0xffff100000000001)
)

func (s *shims) registerPlatformServices() error {
	services := []struct {
		names   []string
		handler shimHandler
	}{
		{[]string{"_CFBundleGetMainBundle", "_CFDataGetBytePtr", "_CFDataGetLength", "_CFStringGetLength", "_CFStringGetMaximumSizeForEncoding", "_CFUUIDCreateString", "_IORegistryEntryFromPath", "_IORegistryEntrySearchCFProperty", "_getenv", "_pthread_self"}, s.returnZero},
		{[]string{"_CFDictionaryGetValue", "_CFFileDescriptorCreate", "_CFFileDescriptorCreateRunLoopSource", "_CFMachPortCreateRunLoopSource", "_CFRunLoopGetCurrent", "_CFRunLoopGetMain", "_CFURLCreateFromFileSystemRepresentation", "_DADiskCopyDescription", "_DADiskCreateFromBSDName", "_DASessionCreate", "_IORegistryEntryCreateCFProperty", "_dispatch_get_global_queue", "_dispatch_queue_create", "_dispatch_queue_create_with_target", "_os_transaction_create", "_xpc_connection_create"}, s.returnFakeHandle},
		{[]string{"_CFRelease", "_CFFileDescriptorEnableCallBacks", "_CFFileDescriptorInvalidate", "_CFFileSecuritySetMode", "_CFRunLoopAddSource", "_CFURLCopyResourcePropertyForKey", "_CFURLSetResourcePropertyForKey", "_CSBackupIsItemExcluded", "_CSBackupSetItemExcluded", "_IOObjectRelease", "_IOLockLock", "_IOLockUnlock", "_IOLockWakeup", "_IORWLockRead", "_IORWLockWrite", "_IORWLockUnlock", "_lck_mtx_lock", "_lck_mtx_unlock", "_lck_rw_lock_exclusive", "_lck_rw_unlock_exclusive", "_chmod", "_close", "_close$UNIX2003", "_fchmod", "_fcntl", "_fcntl$UNIX2003", "_kevent", "_lockf", "_mkdir", "_pthread_mutex_lock", "_pthread_mutex_unlock", "_pthread_rwlock_init", "_pthread_rwlock_init$UNIX2003", "_pthread_rwlock_destroy", "_pthread_rwlock_rdlock", "_pthread_rwlock_unlock", "_pthread_rwlock_unlock$UNIX2003", "_pthread_rwlock_wrlock", "_pthread_rwlock_wrlock$UNIX2003", "_pthread_set_qos_class_self_np", "_CFRunLoopStop", "_getpid", "_getuid", "_notify_post", "_ptrace", "_sched_yield", "_signal", "_umask", "_os_release", "_xpc_connection_activate", "_xpc_connection_cancel", "_xpc_connection_resume", "_xpc_connection_set_target_queue", "_xpc_dictionary_set_bool", "_xpc_dictionary_set_int64", "_xpc_dictionary_set_string", "_xpc_dictionary_set_uint64", "_xpc_dictionary_set_value", "_xpc_release", "_xpc_retain", "_xpc_transaction_begin", "_xpc_transaction_end"}, s.returnZero},
		{[]string{"_IOLog", "_IOLogv"}, s.returnZero},
		{[]string{"_notify_register_check"}, s.notifyRegisterCheck},
		{[]string{"_notify_check"}, s.notifyCheck},
		{[]string{"_notify_get_state"}, s.notifyGetState},
		{[]string{"_notify_cancel"}, s.returnZero},
		{[]string{"_atexit"}, s.returnZero},
		{[]string{"_CFRunLoopRun"}, s.cfRunLoopRun},
		{[]string{"_CFMachPortCreateWithPort"}, s.cfMachPortCreateWithPort},
		{[]string{"_CFMachPortGetPort"}, s.returnOne},
		{[]string{"_SecTaskCreateWithAuditToken", "_SecTaskCopyValueForEntitlement"}, s.returnFakeHandle},
		{[]string{"_CFStringCreateWithCString"}, s.cfStringCreate},
		{[]string{"_CFStringCreateWithCStringNoCopy"}, s.cfStringCreateNoCopy},
		{[]string{"_CFStringGetCString"}, s.cfStringGetCString},
		{[]string{"_CFGetTypeID", "_CFNumberGetTypeID"}, s.returnOne},
		{[]string{"_CFStringGetTypeID"}, s.returnTwo},
		{[]string{"_CFDictionaryGetTypeID"}, s.returnThree},
		{[]string{"_CFNumberGetValue"}, s.cfNumberGetValue},
		{[]string{"___chkstk_darwin", "____chkstk_darwin"}, s.returnUnchanged},
		{[]string{"__ZN9IOService15serviceMatchingEPKcP12OSDictionary"}, s.returnUnchanged},
		{[]string{"__ZN9IOService14waitForServiceEP12OSDictionaryP13mach_timespec"}, s.waitForKernelService},
		{[]string{"__ZN24IOBufferMemoryDescriptor17inTaskWithOptionsEP4taskjmm"}, s.newKernelBufferDescriptor},
		{[]string{"__ZN18IOMemoryDescriptor16withAddressRangeEyyjP4task"}, s.newKernelBufferDescriptorAt},
		{[]string{"_IOIteratorNext"}, s.ioIteratorNext},
		{[]string{"_IOConnectCallStructMethod"}, s.ioConnectCallStructMethod},
		{[]string{"_IORegistryEntryGetParentEntry"}, s.ioRegistryEntryGetParentEntry},
		{[]string{"_IOServiceGetMatchingServices"}, s.ioServiceGetMatchingServices},
		{[]string{"_IOServiceGetMatchingService"}, s.returnOne},
		{[]string{"_IOServiceMatching"}, s.returnFakeHandle},
		{[]string{"_IOMainPort"}, s.ioMainPort},
		{[]string{"_IOServiceOpen"}, s.ioServiceOpen},
		{[]string{"_OSAtomicCompareAndSwap32Barrier"}, s.compareAndSwap32},
		{[]string{"___error"}, s.errorPointer},
		{[]string{"_abort", "___stack_chk_fail", "dyld_stub_binder"}, s.abort},
		{[]string{"_arc4random"}, s.arc4random},
		{[]string{"_random"}, s.random31},
		{[]string{"_arc4random_uniform"}, s.arc4randomUniform},
		{[]string{"_read_random", "_kernel_read_random"}, s.readRandom},
		{[]string{"_audit_token_to_au32"}, s.auditTokenToAU32},
		{[]string{"_audit_token_to_euid"}, s.returnZero},
		{[]string{"_basename"}, s.basename},
		{[]string{"_bootstrap_look_up"}, s.bootstrapLookUp},
		{[]string{"_bootstrap_check_in"}, s.bootstrapCheckIn},
		{[]string{"_mach_msg_rpc_from_kernel_proper"}, s.machMsgRPCFromKernel},
		{[]string{"_mach_msg", "_mach_msg2_internal"}, s.machMsg},
		{[]string{"_mach_msg_destroy", "_mach_msg_destroy_from_kernel_proper", "_mach_port_deallocate", "_mig_dealloc_reply_port", "_mig_deallocate", "_mig_put_reply_port"}, s.returnZero},
		{[]string{"_mig_get_reply_port"}, s.returnOne},
		{[]string{"_dispatch_once"}, s.dispatchOnce},
		{[]string{"_dlopen"}, s.dlopen},
		{[]string{"_dlsym"}, s.dlsym},
		{[]string{"_statfs", "_statfs$INODE64"}, s.returnMinusOne},
		{[]string{"_fstat"}, s.fstat},
		{[]string{"_ftruncate"}, s.ftruncate},
		{[]string{"_gettimeofday"}, s.gettimeofday},
		{[]string{"_clock_get_calendar_microtime"}, s.clockGetCalendarMicrotime},
		{[]string{"_time"}, s.currentTime},
		{[]string{"_gethostuuid"}, s.gethostuuid},
		{[]string{"_objc_msgSend"}, s.objcMsgSend},
		{[]string{"_open", "_open$UNIX2003"}, s.open},
		{[]string{"_lstat", "_lstat$INODE64", "_stat", "_stat$INODE64"}, s.lstat},
		{[]string{"_pthread_once"}, s.pthreadOnce},
		{[]string{"_proc_pidpath"}, s.procPIDPath},
		{[]string{"_read", "_read$UNIX2003"}, s.read},
		{[]string{"_write"}, s.write},
		{[]string{"_unlink"}, s.unlink},
		{[]string{"_sysctl"}, s.returnMinusOne},
		{[]string{"_sysctlbyname"}, s.sysctlbyname},
		{[]string{"_sysconf"}, s.sysconf},
		{[]string{"_xpc_dictionary_get_data"}, s.xpcDictionaryGetData},
		{[]string{"_xpc_dictionary_set_data"}, s.xpcDictionarySetData},
		{[]string{"_xpc_connection_create_mach_service"}, s.xpcConnectionCreateMachService},
		{[]string{"_xpc_connection_send_message_with_reply_sync"}, s.xpcConnectionSendMessageWithReplySync},
		{[]string{"_xpc_connection_set_event_handler"}, s.xpcConnectionSetEventHandler},
		{[]string{"_xpc_dictionary_create_empty"}, s.xpcDictionaryCreateEmpty},
		{[]string{"_xpc_dictionary_create_reply"}, s.xpcDictionaryCreateReply},
		{[]string{"_xpc_dictionary_get_audit_token"}, s.xpcDictionaryGetAuditToken},
		{[]string{"_xpc_dictionary_send_reply"}, s.xpcDictionarySendReply},
		{[]string{"_xpc_get_type"}, s.xpcGetType},
		{[]string{"_kqueue"}, s.kqueue},
		{[]string{"_os_log_type_enabled"}, s.returnOne},
		{[]string{"_os_log_impl", "__os_log_impl"}, s.returnZero},
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
		"_NDR_record",
		"_kCFAllocatorDefault",
		"_kCFAllocatorNull",
		"_kDADiskDescriptionVolumeUUIDKey",
		"_kIOMasterPortDefault",
	} {
		if _, err := s.addData(name, make([]byte, 8)); err != nil {
			return err
		}
	}

	for name, value := range map[string]uint64{
		"_kCFBooleanFalse":               fakeHandle,
		"_kCFBooleanTrue":                fakeHandle,
		"__xpc_type_dictionary":          xpcDictionaryType,
		"__xpc_type_error":               xpcDictionaryType + 1,
		"__xpc_error_connection_invalid": xpcDictionaryType + 2,
	} {
		var data [8]byte
		binary.LittleEndian.PutUint64(data[:], value)
		if _, err := s.addData(name, data[:]); err != nil {
			return err
		}
	}

	if _, err := s.addFunction("callback.complete", s.callbackComplete); err != nil {
		return err
	}

	return nil
}

func (s *shims) machMsgRPCFromKernel() error {
	message, err := s.argument(0)
	if err != nil {
		return err
	}
	sendSize, err := s.argument(1)
	if err != nil {
		return err
	}
	receiveSize, err := s.argument(2)
	if err != nil {
		return err
	}
	if sendSize == 0 || sendSize > 1<<20 || receiveSize > 1<<20 {
		return errors.New("kernel Mach RPC size is invalid")
	}
	request, err := s.engine.MemRead(message, sendSize)
	if err != nil {
		return fmt.Errorf("read kernel Mach RPC request: %w", err)
	}
	if s.machRPCHandler == nil {
		return errors.New("kernel Mach RPC handler is unavailable")
	}
	reply, err := s.machRPCHandler(request, int(receiveSize))
	if err != nil {
		return err
	}
	if len(reply) == 0 || len(reply) > int(receiveSize) {
		return errors.New("kernel Mach RPC reply is invalid")
	}
	if err := s.engine.MemWrite(message, reply); err != nil {
		return fmt.Errorf("write kernel Mach RPC reply: %w", err)
	}
	return s.setResult(0)
}

func (s *shims) machMsg() error {
	message, err := s.argument(0)
	if err != nil {
		return err
	}
	sendSize, err := s.argument(2)
	if err != nil {
		return err
	}
	if sendSize == 0 || sendSize > 1<<20 {
		return s.setResult(0)
	}
	reply, err := s.engine.MemRead(message, sendSize)
	if err != nil {
		return fmt.Errorf("read Mach message: %w", err)
	}
	s.machMessageReply = append([]byte(nil), reply...)

	return s.setResult(0)
}

func (s *shims) kqueue() error {
	return s.setResult(6)
}

func (s *shims) cfRunLoopRun() error {
	if !s.pauseRunLoop {
		return s.returnZero()
	}
	if err := s.engine.Stop(); err != nil {
		return fmt.Errorf("pause guest run loop: %w", err)
	}

	return nil
}

func (s *shims) cfMachPortCreateWithPort() error {
	callback, err := s.argument(2)
	if err != nil {
		return err
	}
	context, err := s.argument(3)
	if err != nil {
		return err
	}
	s.machPortCallback = callback
	s.machPortContext = context
	if context != 0 {
		s.machPortInfo, _ = s.readUint64(context + 8)
	}
	shouldFreeInfo, err := s.argument(4)
	if err == nil && shouldFreeInfo != 0 {
		if writeErr := s.engine.MemWrite(shouldFreeInfo, []byte{0}); writeErr != nil {
			return fmt.Errorf("initialize Mach port release flag: %w", writeErr)
		}
	}

	return s.setResult(fakeHandle)
}

func (s *shims) newXPCObject(data []byte) uint64 {
	s.xpcNextObject++
	s.xpcObjects[s.xpcNextObject] = &xpcObject{data: append([]byte(nil), data...), hasData: data != nil}

	return s.xpcNextObject
}

func (s *shims) xpcConnectionSendMessageWithReplySync() error {
	object := s.newXPCObject(s.xpcSyncReply)

	return s.setResult(object)
}

func (s *shims) xpcConnectionCreateMachService() error {
	return s.setResult(s.newXPCObject(nil))
}

func (s *shims) xpcConnectionSetEventHandler() error {
	block, err := s.argument(1)
	if err != nil {
		return err
	}

	copyAddress, err := s.allocate(64)
	if err != nil {
		return err
	}
	data, err := s.engine.MemRead(block, 64)
	if err != nil {
		return fmt.Errorf("copy XPC event handler: %w", err)
	}
	if err := s.engine.MemWrite(copyAddress, data); err != nil {
		return fmt.Errorf("store XPC event handler: %w", err)
	}
	s.xpcEventHandler = copyAddress

	return s.setResult(0)
}

func (s *shims) xpcDictionaryCreateEmpty() error {
	return s.setResult(s.newXPCObject(nil))
}

func (s *shims) xpcDictionaryCreateReply() error {
	return s.setResult(s.newXPCObject(nil))
}

func (s *shims) xpcDictionaryGetAuditToken() error {
	token, err := s.argument(1)
	if err != nil {
		return err
	}
	if token != 0 {
		value := make([]byte, 32)
		binary.LittleEndian.PutUint32(value[0:4], math.MaxUint32)
		binary.LittleEndian.PutUint32(value[20:24], 123)
		binary.LittleEndian.PutUint32(value[24:28], 100000)
		binary.LittleEndian.PutUint32(value[28:32], 1)
		if err := s.engine.MemWrite(token, value); err != nil {
			return fmt.Errorf("write XPC audit token: %w", err)
		}
	}

	return s.setResult(0)
}

func (s *shims) xpcDictionarySendReply() error {
	object, err := s.argument(0)
	if err != nil {
		return err
	}
	if value := s.xpcObjects[object]; value != nil {
		s.xpcReply = append(s.xpcReply[:0], value.data...)
	}
	s.xpcReplied = true

	return s.setResult(0)
}

func (s *shims) xpcGetType() error {
	object, err := s.argument(0)
	if err != nil {
		return err
	}
	if s.xpcObjects[object] != nil {
		return s.setResult(s.symbols["__xpc_type_dictionary"])
	}

	return s.setResult(0)
}

func (s *shims) xpcDictionarySetData() error {
	object, err := s.argument(0)
	if err != nil {
		return err
	}
	address, err := s.argument(2)
	if err != nil {
		return err
	}
	length, err := s.argument(3)
	if err != nil {
		return err
	}
	if length > 1<<20 {
		return errors.New("XPC data exceeds guest limit")
	}
	s.xpcData, err = s.engine.MemRead(address, length)
	if err != nil {
		return fmt.Errorf("read XPC data: %w", err)
	}
	if value := s.xpcObjects[object]; value != nil {
		value.data = append(value.data[:0], s.xpcData...)
		value.hasData = true
	}

	return s.setResult(0)
}

func (s *shims) xpcDictionaryGetData() error {
	object, err := s.argument(0)
	if err != nil {
		return err
	}
	length, err := s.argument(2)
	if err != nil {
		return err
	}
	value := s.xpcObjects[object]
	if value == nil || !value.hasData {
		if length != 0 {
			if err := s.writeUint64(length, 0); err != nil {
				return err
			}
		}

		return s.setResult(0)
	}
	if value.dataAddress == 0 {
		value.dataAddress, err = s.allocate(uint64(max(len(value.data), 1)))
		if err != nil {
			return err
		}
		if len(value.data) != 0 {
			if err := s.engine.MemWrite(value.dataAddress, value.data); err != nil {
				return fmt.Errorf("write XPC data: %w", err)
			}
		}
	}
	if length != 0 {
		if err := s.writeUint64(length, uint64(len(value.data))); err != nil {
			return err
		}
	}

	return s.setResult(value.dataAddress)
}

func (s *shims) returnZero() error {
	return s.setResult(0)
}

func (s *shims) returnOne() error {
	return s.setResult(1)
}

func (s *shims) returnTwo() error {
	return s.setResult(2)
}

func (s *shims) returnThree() error {
	return s.setResult(3)
}

func (s *shims) returnUnchanged() error {
	return nil
}

func (s *shims) returnFakeHandle() error {
	return s.setResult(fakeHandle)
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

func (s *shims) cfNumberGetValue() error {
	address, err := s.argument(2)
	if err != nil {
		return err
	}
	if address != 0 {
		if err := s.writeUint32(address, 3184554750); err != nil {
			return err
		}
	}

	return s.setResult(1)
}

func (s *shims) cfStringCreateNoCopy() error {
	address, err := s.argument(1)
	if err != nil {
		return err
	}

	return s.setResult(address)
}

func (s *shims) cfStringGetCString() error {
	valueAddress, err := s.argument(0)
	if err != nil {
		return err
	}
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

	value := ""
	if valueAddress != 0 && valueAddress != fakeHandle {
		if decoded, readErr := s.readCString(valueAddress); readErr == nil {
			value = decoded
		}
	}
	encoded := append([]byte(value), 0)
	if uint64(len(encoded)) > capacity {
		return s.setResult(0)
	}
	if err := s.engine.MemWrite(buffer, encoded); err != nil {
		return fmt.Errorf("write guest string buffer: %w", err)
	}

	return s.setResult(1)
}

func (s *shims) ioIteratorNext() error {
	s.iterator++

	return s.setResult(uint64(s.iterator % 2))
}

func (s *shims) ioConnectCallStructMethod() error {
	selector, err := s.argument(1)
	if err != nil {
		return err
	}
	inputSize, err := s.argument(3)
	if err != nil {
		return err
	}
	outputSizeAddress, err := s.argument(5)
	if err != nil {
		return err
	}
	outputSize := uint64(0)
	if outputSizeAddress != 0 {
		outputSize, _ = s.readUint64(outputSizeAddress)
	}
	if inputSize > 1<<20 || outputSize > 1<<20 {
		return errors.New("FairPlay IOKit buffer exceeds guest limit")
	}
	inputAddress, err := s.argument(2)
	if err != nil {
		return err
	}
	input, err := s.engine.MemRead(inputAddress, inputSize)
	if err != nil {
		return fmt.Errorf("read FairPlay IOKit input: %w", err)
	}
	var indirect []byte
	var indirectAddress uint64
	if selector == 26 && len(input) == 16 {
		indirectAddress = binary.LittleEndian.Uint64(input[:8])
		indirectSize := binary.LittleEndian.Uint64(input[8:])
		if indirectSize == 0 || indirectSize > 1<<20 {
			return errors.New("FairPlay IOKit indirect buffer is invalid")
		}
		indirect, err = s.engine.MemRead(indirectAddress, indirectSize)
		if err != nil {
			return fmt.Errorf("read FairPlay IOKit indirect buffer: %w", err)
		}
	}
	var output, indirectOutput []byte
	var code int32
	if s.fairPlayHandler == nil {
		return errors.New("emulated FairPlay IOKit handler is unavailable")
	}
	output, indirectOutput, code, hostErr := s.fairPlayHandler(input, indirect, int(outputSize))
	if hostErr == nil && code != 0 && selector == 26 && len(indirect) >= 16 && len(input) == int(outputSize) {
		indirectOutput = append([]byte(nil), indirect...)
		digest := sha256.Sum256(indirectOutput[:len(indirectOutput)-16])
		copy(indirectOutput[len(indirectOutput)-16:], digest[:16])
		output = append([]byte(nil), input...)
		code = 0
	}
	if hostErr != nil {
		return hostErr
	}
	if len(indirectOutput) != 0 {
		if err := s.engine.MemWrite(indirectAddress, indirectOutput); err != nil {
			return fmt.Errorf("write FairPlay IOKit indirect buffer: %w", err)
		}
		if len(output) >= 8 {
			binary.LittleEndian.PutUint64(output[:8], indirectAddress)
		}
	}
	outputAddress, err := s.argument(4)
	if err != nil {
		return err
	}
	if len(output) != 0 {
		if err := s.engine.MemWrite(outputAddress, output); err != nil {
			return fmt.Errorf("write FairPlay IOKit output: %w", err)
		}
	}
	if outputSizeAddress != 0 {
		if err := s.writeUint64(outputSizeAddress, uint64(len(output))); err != nil {
			return err
		}
	}

	return s.setResult(uint64(uint32(code)))
}

func (s *shims) ioMainPort() error {
	port, err := s.argument(1)
	if err != nil {
		return err
	}
	if port != 0 {
		if err := s.writeUint32(port, 1); err != nil {
			return err
		}
	}

	return s.setResult(0)
}

func (s *shims) waitForKernelService() error {
	nameAddress, err := s.argument(0)
	if err != nil {
		return err
	}
	name, err := s.readCString(nameAddress)
	if err != nil {
		return err
	}
	if name != "IOAESAccelerator" {
		return s.setResult(0)
	}
	if s.aesService != 0 {
		return s.setResult(s.aesService)
	}

	unhandled, err := s.addFunction("$IOAESAccelerator.unhandled", s.returnZero)
	if err != nil {
		return err
	}
	retain, err := s.addFunction("$IOAESAccelerator.retain", s.returnUnchanged)
	if err != nil {
		return err
	}
	perform, err := s.addFunction("$IOAESAccelerator.performAES", s.ioAESPerform)
	if err != nil {
		return err
	}
	vtable := make([]byte, 0x900)
	for offset := 0; offset < len(vtable); offset += 8 {
		binary.LittleEndian.PutUint64(vtable[offset:offset+8], unhandled)
	}
	binary.LittleEndian.PutUint64(vtable[0x20:0x28], retain)
	binary.LittleEndian.PutUint64(vtable[0x550:0x558], perform)
	binary.LittleEndian.PutUint64(vtable[0x890:0x898], perform)
	s.aesVtable, err = s.addData("kernel.ioaes.vtable", vtable)
	if err != nil {
		return err
	}
	s.aesService, err = s.allocate(16)
	if err != nil {
		return err
	}
	if err := s.writeUint64(s.aesService, s.aesVtable); err != nil {
		return err
	}

	return s.setResult(s.aesService)
}

func (s *shims) newKernelBufferDescriptor() error {
	capacity, err := s.argument(2)
	if err != nil {
		return err
	}
	if capacity == 0 || capacity > maxGuestTransfer {
		return s.setResult(0)
	}
	buffer, err := s.allocateAligned(capacity, 16)
	if err != nil {
		return err
	}
	if err := s.engine.MemWrite(buffer, make([]byte, capacity)); err != nil {
		return fmt.Errorf("initialize kernel buffer: %w", err)
	}

	return s.createKernelBufferDescriptor(buffer, capacity)
}

func (s *shims) newKernelBufferDescriptorAt() error {
	buffer, err := s.argument(0)
	if err != nil {
		return err
	}
	capacity, err := s.argument(1)
	if err != nil {
		return err
	}
	if buffer == 0 || capacity == 0 || capacity > maxGuestTransfer {
		return s.setResult(0)
	}

	return s.createKernelBufferDescriptor(buffer, capacity)
}

func (s *shims) createKernelBufferDescriptor(buffer, capacity uint64) error {
	if s.bufferVtable == 0 {
		unhandled, err := s.addFunction("$IOBufferMemoryDescriptor.unhandled", s.unsupportedKernelBufferMethod)
		if err != nil {
			return err
		}
		prepare, err := s.addFunction("$IOBufferMemoryDescriptor.prepare", s.kernelBufferPrepare)
		if err != nil {
			return err
		}
		complete, err := s.addFunction("$IOBufferMemoryDescriptor.complete", s.returnZero)
		if err != nil {
			return err
		}
		address, err := s.addFunction("$IOBufferMemoryDescriptor.getVirtualAddress", s.kernelBufferAddress)
		if err != nil {
			return err
		}
		read, err := s.addFunction("$IOMemoryDescriptor.readBytes", s.kernelBufferRead)
		if err != nil {
			return err
		}
		write, err := s.addFunction("$IOMemoryDescriptor.writeBytes", s.kernelBufferWrite)
		if err != nil {
			return err
		}
		release, err := s.addFunction("$IOBufferMemoryDescriptor.release", s.returnZero)
		if err != nil {
			return err
		}
		vtable := make([]byte, 0x1000)
		for offset := 0; offset < len(vtable); offset += 8 {
			binary.LittleEndian.PutUint64(vtable[offset:offset+8], unhandled)
		}
		binary.LittleEndian.PutUint64(vtable[0x28:0x30], release)
		binary.LittleEndian.PutUint64(vtable[0xc8:0xd0], read)
		binary.LittleEndian.PutUint64(vtable[0xd0:0xd8], write)
		binary.LittleEndian.PutUint64(vtable[0xd8:0xe0], prepare)
		binary.LittleEndian.PutUint64(vtable[0xe0:0xe8], complete)
		binary.LittleEndian.PutUint64(vtable[0x148:0x150], address)
		binary.LittleEndian.PutUint64(vtable[0x218:0x220], prepare)
		binary.LittleEndian.PutUint64(vtable[0x220:0x228], complete)
		binary.LittleEndian.PutUint64(vtable[0x308:0x310], address)
		s.bufferVtable, err = s.addData("kernel.iobuffer.vtable", vtable)
		if err != nil {
			return err
		}
	}
	descriptor, err := s.allocate(24)
	if err != nil {
		return err
	}
	if err := s.writeUint64(descriptor, s.bufferVtable); err != nil {
		return err
	}
	if err := s.writeUint64(descriptor+8, buffer); err != nil {
		return err
	}
	if err := s.writeUint64(descriptor+16, capacity); err != nil {
		return err
	}

	return s.setResult(descriptor)
}

func (s *shims) unsupportedKernelBufferMethod() error {
	caller, _ := s.engine.RegRead(unicorn.RegLR)

	return fmt.Errorf("unsupported IOMemoryDescriptor virtual method from %#x", caller)
}

func (s *shims) kernelBufferPrepare() error {
	descriptor, err := s.argument(0)
	if err != nil {
		return err
	}
	buffer, err := s.readUint64(descriptor + 8)
	if err != nil {
		return err
	}
	capacity, err := s.readUint64(descriptor + 16)
	if err != nil {
		return err
	}
	if buffer == 0 || capacity == 0 || capacity > maxGuestTransfer {
		return s.setResult(3)
	}

	return s.setResult(0)
}

func (s *shims) kernelBufferAddress() error {
	descriptor, err := s.argument(0)
	if err != nil {
		return err
	}
	address, err := s.readUint64(descriptor + 8)
	if err != nil {
		return err
	}

	return s.setResult(address)
}

func (s *shims) kernelBufferRead() error {
	return s.transferKernelBuffer(false)
}

func (s *shims) kernelBufferWrite() error {
	return s.transferKernelBuffer(true)
}

func (s *shims) transferKernelBuffer(write bool) error {
	descriptor, err := s.argument(0)
	if err != nil {
		return err
	}
	offset, err := s.argument(1)
	if err != nil {
		return err
	}
	address, err := s.argument(2)
	if err != nil {
		return err
	}
	length, err := s.argument(3)
	if err != nil {
		return err
	}
	buffer, err := s.readUint64(descriptor + 8)
	if err != nil {
		return err
	}
	capacity, err := s.readUint64(descriptor + 16)
	if err != nil {
		return err
	}
	if offset > capacity || length > capacity-offset || length > maxGuestTransfer {
		return s.setResult(0)
	}
	source, destination := buffer+offset, address
	if write {
		source, destination = address, buffer+offset
	}
	data, err := s.engine.MemRead(source, length)
	if err != nil {
		return fmt.Errorf("read kernel buffer transfer: %w", err)
	}
	if err := s.engine.MemWrite(destination, data); err != nil {
		return fmt.Errorf("write kernel buffer transfer: %w", err)
	}

	return s.setResult(length)
}

func (s *shims) ioAESPerform() error {
	arguments := make([]uint64, 8)
	for index := range arguments {
		value, err := s.argument(index)
		if err != nil {
			return err
		}
		arguments[index] = value
	}
	length := arguments[3]
	if length == 0 || length > maxGuestTransfer || length%16 != 0 {
		return s.setResult(3)
	}
	sourceBase, err := s.readUint64(arguments[1] + 8)
	if err != nil {
		return err
	}
	sourceCapacity, err := s.readUint64(arguments[1] + 16)
	if err != nil {
		return err
	}
	destinationBase, err := s.readUint64(arguments[2] + 8)
	if err != nil {
		return err
	}
	destinationCapacity, err := s.readUint64(arguments[2] + 16)
	if err != nil {
		return err
	}
	destinationOffset, err := s.argument(8)
	if err != nil {
		return err
	}
	if arguments[7] > sourceCapacity || length > sourceCapacity-arguments[7] || destinationOffset > destinationCapacity || length > destinationCapacity-destinationOffset {
		return s.setResult(3)
	}
	source, err := s.engine.MemRead(sourceBase+arguments[7], length)
	if err != nil {
		return fmt.Errorf("read IOAES input: %w", err)
	}
	iv, err := s.engine.MemRead(arguments[4], 16)
	if err != nil {
		return fmt.Errorf("read IOAES initialization vector: %w", err)
	}
	key, err := s.engine.MemRead(arguments[6], 40)
	if err != nil {
		return fmt.Errorf("read IOAES key data: %w", err)
	}
	destination, outputIV, err := emulateIOAES(source, iv, uint32(arguments[5]), key)
	if err != nil {
		return err
	}
	if len(destination) != len(source) || len(outputIV) != 16 {
		return errors.New("emulated IOAES returned invalid output")
	}
	if err := s.engine.MemWrite(destinationBase+destinationOffset, destination); err != nil {
		return fmt.Errorf("write IOAES output: %w", err)
	}
	if err := s.engine.MemWrite(arguments[4], outputIV); err != nil {
		return fmt.Errorf("write IOAES initialization vector: %w", err)
	}

	return s.setResult(0)
}

func (s *shims) ioServiceOpen() error {
	connection, err := s.argument(3)
	if err != nil {
		return err
	}
	if connection != 0 {
		if err := s.writeUint32(connection, 7); err != nil {
			return err
		}
	}

	return s.setResult(0)
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

func (s *shims) random31() error {
	var value [4]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Errorf("generate guest random value: %w", err)
	}

	return s.setResult(uint64(binary.LittleEndian.Uint32(value[:]) & math.MaxInt32))
}

func (s *shims) arc4randomUniform() error {
	upper, err := s.argument(0)
	if err != nil {
		return err
	}
	if upper < 2 {
		return s.setResult(0)
	}

	limit := uint64(math.MaxUint32) + 1
	limit -= limit % upper
	for {
		var data [4]byte
		if _, err := rand.Read(data[:]); err != nil {
			return fmt.Errorf("generate bounded guest random value: %w", err)
		}
		value := uint64(binary.LittleEndian.Uint32(data[:]))
		if value < limit {
			return s.setResult(value % upper)
		}
	}
}

func (s *shims) readRandom() error {
	address, err := s.argument(0)
	if err != nil {
		return err
	}
	length, err := s.argument(1)
	if err != nil {
		return err
	}
	if length > maxGuestTransfer {
		return fmt.Errorf("random request size %d exceeds limit", length)
	}
	data := make([]byte, length)
	if _, err := rand.Read(data); err != nil {
		return fmt.Errorf("generate guest random bytes: %w", err)
	}
	if err := s.engine.MemWrite(address, data); err != nil {
		return fmt.Errorf("write guest random bytes: %w", err)
	}

	return s.setResult(0)
}

func (s *shims) notifyRegisterCheck() error {
	token, err := s.argument(1)
	if err != nil {
		return err
	}
	if token != 0 {
		if err := s.writeUint32(token, 1); err != nil {
			return err
		}
	}

	return s.setResult(0)
}

func (s *shims) notifyCheck() error {
	changed, err := s.argument(1)
	if err != nil {
		return err
	}
	if changed != 0 {
		if err := s.writeUint32(changed, 0); err != nil {
			return err
		}
	}

	return s.setResult(0)
}

func (s *shims) notifyGetState() error {
	state, err := s.argument(1)
	if err != nil {
		return err
	}
	if state != 0 {
		if err := s.writeUint64(state, 0); err != nil {
			return err
		}
	}

	return s.setResult(0)
}

func (s *shims) auditTokenToAU32() error {
	values := [...]uint32{0, 0, 0, 0, 0, 123, 100000, 1}
	for index, value := range values {
		address, err := s.argument(index + 1)
		if err != nil {
			return err
		}
		if address != 0 {
			if err := s.writeUint32(address, value); err != nil {
				return err
			}
		}
	}

	return s.setResult(0)
}

func (s *shims) basename() error {
	address, err := s.argument(0)
	if err != nil {
		return err
	}

	value, err := s.readCString(address)
	if err != nil {
		return err
	}

	value = strings.TrimRight(value, "/")
	if index := strings.LastIndexByte(value, '/'); index >= 0 {
		address += uint64(index + 1)
	}

	return s.setResult(address)
}

func (s *shims) lstat() error {
	pathAddress, err := s.argument(0)
	if err != nil {
		return err
	}
	path, err := s.readCString(pathAddress)
	if err != nil {
		return err
	}
	isDirectory := path == "/var/db/fpsd/adi" || path == "/var/db" || path == "/var/setup"
	_, isFile := s.adiFiles[path]
	if isDirectory {
		// The daemon owns this directory on macOS. It is virtual in the VM.
	} else if isADIStatePath(path) && isFile {
		// The daemon has created its persistent state file.
	} else {
		return s.returnMinusOne()
	}

	result, err := s.argument(1)
	if err != nil {
		return err
	}
	mode := []byte{0xed, 0x41}
	if isFile {
		mode = []byte{0x80, 0x81}
	}
	// Darwin's stat structure stores st_mode at byte offset four.
	if err := s.engine.MemWrite(result+4, mode); err != nil {
		return fmt.Errorf("write guest stat mode: %w", err)
	}

	return s.setResult(0)
}

func (s *shims) bootstrapLookUp() error {
	service, err := s.argument(2)
	if err != nil {
		return err
	}

	if service != 0 {
		if err := s.writeUint32(service, 0); err != nil {
			return err
		}
	}

	return s.setResult(5) // KERN_FAILURE
}

func (s *shims) bootstrapCheckIn() error {
	port, err := s.argument(2)
	if err != nil {
		return err
	}
	if port != 0 {
		if err := s.writeUint32(port, 1); err != nil {
			return err
		}
	}

	return s.setResult(0)
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

func (s *shims) currentTime() error {
	address, err := s.argument(0)
	if err != nil {
		return err
	}

	seconds := uint64(time.Now().Unix())
	if address != 0 {
		if err := s.writeUint64(address, seconds); err != nil {
			return err
		}
	}

	return s.setResult(seconds)
}

func (s *shims) clockGetCalendarMicrotime() error {
	secondsAddress, err := s.argument(0)
	if err != nil {
		return err
	}
	microsecondsAddress, err := s.argument(1)
	if err != nil {
		return err
	}

	now := time.Now()
	if err := s.writeUint32(secondsAddress, uint32(now.Unix())); err != nil {
		return err
	}
	if err := s.writeUint32(microsecondsAddress, uint32(now.Nanosecond()/1000)); err != nil {
		return err
	}

	return s.setResult(0)
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

func (s *shims) gethostuuid() error {
	if s.hostUUID == [16]byte{} {
		return errors.New("guest host UUID is unavailable")
	}

	address, err := s.argument(0)
	if err != nil {
		return err
	}
	if address != 0 {
		if err := s.engine.MemWrite(address, s.hostUUID[:]); err != nil {
			return fmt.Errorf("write guest host UUID: %w", err)
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
	flags, err := s.argument(1)
	if err != nil {
		return err
	}
	if path == "/var/db" || path == "/var/setup" {
		return s.setResult(4)
	}
	if isADIStatePath(path) {
		if _, exists := s.adiFiles[path]; !exists {
			if flags&0x200 == 0 { // O_CREAT
				return s.returnMinusOne()
			}
			s.adiFiles[path] = []byte{}
		}
		s.adiPath = path
		s.adiOffset = 0

		return s.setResult(adiFile)
	}

	if path == icxsPath {
		s.icxsOffset = 0

		return s.setResult(coreFPFile)
	}

	return s.returnMinusOne()
}

func (s *shims) fstat() error {
	descriptor, err := s.argument(0)
	if err != nil {
		return err
	}
	if descriptor != adiFile {
		return s.returnMinusOne()
	}
	result, err := s.argument(1)
	if err != nil {
		return err
	}
	if err := s.engine.MemWrite(result, make([]byte, 144)); err != nil {
		return fmt.Errorf("clear guest stat: %w", err)
	}
	if err := s.engine.MemWrite(result+4, []byte{0x80, 0x81}); err != nil {
		return fmt.Errorf("write guest file mode: %w", err)
	}
	if err := s.writeUint64(result+96, uint64(len(s.adiFiles[s.adiPath]))); err != nil {
		return err
	}

	return s.setResult(0)
}

func (s *shims) ftruncate() error {
	descriptor, err := s.argument(0)
	if err != nil {
		return err
	}
	if descriptor != adiFile {
		return s.returnMinusOne()
	}
	length, err := s.argument(1)
	if err != nil {
		return err
	}
	if length > 1<<20 {
		return s.returnMinusOne()
	}
	data := s.adiFiles[s.adiPath]
	if length <= uint64(len(data)) {
		data = data[:length]
	} else {
		data = append(data, make([]byte, int(length)-len(data))...)
	}
	s.adiFiles[s.adiPath] = data
	if s.adiOffset > len(data) {
		s.adiOffset = len(data)
	}

	return s.setResult(0)
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

	if value == 0 || value == 0x4f4e4345 {
		return s.setResult(0)
	}

	if err := s.writeUint64(control, 0x4f4e4345); err != nil {
		return err
	}

	return s.invokeCallback(initializer)
}

func (s *shims) procPIDPath() error {
	buffer, err := s.argument(1)
	if err != nil {
		return err
	}
	size, err := s.argument(2)
	if err != nil {
		return err
	}
	path := []byte("/System/Library/PrivateFrameworks/CommerceKit.framework/Versions/A/Resources/commerce\x00")
	if buffer == 0 || size < uint64(len(path)) {
		return s.returnMinusOne()
	}
	if err := s.engine.MemWrite(buffer, path); err != nil {
		return fmt.Errorf("write guest process path: %w", err)
	}

	return s.setResult(uint64(len(path) - 1))
}

func (s *shims) dispatchOnce() error {
	predicate, err := s.argument(0)
	if err != nil {
		return err
	}

	block, err := s.argument(1)
	if err != nil {
		return err
	}

	value, err := s.readUint64(predicate)
	if err != nil {
		return err
	}

	if value == math.MaxUint64 {
		return s.setResult(0)
	}

	if err := s.writeUint64(predicate, math.MaxUint64); err != nil {
		return err
	}

	initializer, err := s.readUint64(block + 16)
	if err != nil {
		return err
	}

	if err := s.engine.RegWrite(unicorn.RegX0, block); err != nil {
		return fmt.Errorf("write dispatch block argument: %w", err)
	}

	return s.invokeCallback(initializer)
}

func (s *shims) invokeCallback(initializer uint64) error {
	returnAddress, err := s.engine.RegRead(unicorn.RegLR)
	if err != nil {
		return fmt.Errorf("read guest return register: %w", err)
	}

	s.callbackReturns = append(s.callbackReturns, returnAddress)

	completion := s.symbols["callback.complete"]
	if err := s.engine.RegWrite(unicorn.RegLR, completion); err != nil {
		return fmt.Errorf("write callback return register: %w", err)
	}

	if err := s.engine.RegWrite(unicorn.RegPC, initializer); err != nil {
		return fmt.Errorf("write callback instruction register: %w", err)
	}

	return nil
}

func (s *shims) callbackComplete() error {
	if len(s.callbackReturns) == 0 {
		return errors.New("guest callback completion has no return address")
	}

	last := len(s.callbackReturns) - 1
	returnAddress := s.callbackReturns[last]
	s.callbackReturns = s.callbackReturns[:last]

	if err := s.engine.RegWrite(unicorn.RegLR, returnAddress); err != nil {
		return fmt.Errorf("restore guest return register: %w", err)
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

	if descriptor == adiFile {
		size, sizeErr := checkedSize(requested)
		if sizeErr != nil {
			return sizeErr
		}
		data := s.adiFiles[s.adiPath]
		remaining := len(data) - s.adiOffset
		if size > remaining {
			size = remaining
		}
		if size != 0 {
			if err := s.engine.MemWrite(buffer, data[s.adiOffset:s.adiOffset+size]); err != nil {
				return fmt.Errorf("write ADI state to guest: %w", err)
			}
			s.adiOffset += size
		}

		return s.setResult(uint64(size))
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

func (s *shims) write() error {
	descriptor, err := s.argument(0)
	if err != nil {
		return err
	}
	if descriptor != adiFile {
		return s.returnMinusOne()
	}
	buffer, err := s.argument(1)
	if err != nil {
		return err
	}
	requested, err := s.argument(2)
	if err != nil {
		return err
	}
	size, err := checkedSize(requested)
	if err != nil {
		return err
	}
	data, err := s.engine.MemRead(buffer, uint64(size))
	if err != nil {
		return fmt.Errorf("read ADI state from guest: %w", err)
	}
	state := s.adiFiles[s.adiPath]
	end := s.adiOffset + size
	if end > len(state) {
		state = append(state, make([]byte, end-len(state))...)
	}
	copy(state[s.adiOffset:end], data)
	s.adiFiles[s.adiPath] = state
	s.adiOffset = end

	return s.setResult(uint64(size))
}

func (s *shims) unlink() error {
	pathAddress, err := s.argument(0)
	if err != nil {
		return err
	}
	path, err := s.readCString(pathAddress)
	if err != nil {
		return err
	}
	if isADIStatePath(path) {
		delete(s.adiFiles, path)
		s.adiOffset = 0

		return s.setResult(0)
	}

	return s.returnMinusOne()
}

func isADIStatePath(path string) bool {
	return path == "/var/db/fpsd/adi/adi.pb"
}

func (s *shims) sysctlbyname() error {
	nameAddress, err := s.argument(0)
	if err != nil {
		return err
	}
	name, err := s.readCString(nameAddress)
	if err != nil {
		return err
	}
	output, err := s.argument(1)
	if err != nil {
		return err
	}
	lengthAddress, err := s.argument(2)
	if err != nil {
		return err
	}
	var value []byte
	switch name {
	case "kern.monotonicclock", "hw.ncpu", "hw.logicalcpu", "hw.physicalcpu", "hw.optional.arm64":
		value = make([]byte, 4)
		binary.LittleEndian.PutUint32(value, 1)
	case "hw.memsize":
		value = make([]byte, 8)
		binary.LittleEndian.PutUint64(value, 8<<30)
	case "hw.machine":
		value = []byte("arm64\x00")
	case "hw.model":
		value = []byte("MacBookPro13,3\x00")
	case "kern.osversion":
		value = []byte("16G29\x00")
	default:
		return s.returnMinusOne()
	}

	if lengthAddress != 0 {
		requested, readErr := s.readUint64(lengthAddress)
		if readErr != nil {
			return readErr
		}
		if output != 0 && requested < uint64(len(value)) {
			return s.returnMinusOne()
		}
		if output != 0 {
			if err := s.engine.MemWrite(output, value); err != nil {
				return fmt.Errorf("write sysctl output: %w", err)
			}
		}
		if err := s.writeUint64(lengthAddress, uint64(len(value))); err != nil {
			return err
		}
	}

	return s.setResult(0)
}

func (s *shims) sysconf() error {
	name, err := s.argument(0)
	if err != nil {
		return err
	}

	switch name {
	case 29: // _SC_PAGESIZE
		return s.setResult(pageSize)
	case 57, 58: // _SC_NPROCESSORS_CONF, _SC_NPROCESSORS_ONLN
		return s.setResult(1)
	case 200: // _SC_PHYS_PAGES
		return s.setResult(heapSize / pageSize)
	default:
		return s.returnMinusOne()
	}
}
