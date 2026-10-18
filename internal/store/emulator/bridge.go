package emulator

import (
	"bytes"
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"math"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/majd/ipatool/v2/internal/sap/unicorn"
)

func (r *Runtime) symbol(name string) {
	if r.compressionSymbol(name) {
		return
	}

	switch name {
	case "CC_SHA512":
		length := uint64(uint32(r.arg(1)))
		if length > heapSize {
			panic("SHA-512 input exceeds runtime budget")
		}

		data, err := r.engine.MemRead(r.arg(0), length)
		if err != nil {
			panic(err)
		}

		digest := sha512.Sum512(data)

		output := r.arg(2)
		if output == 0 {
			output = r.alloc(sha512.Size)
		}

		if err = r.MemWrite(output, digest[:]); err != nil {
			panic(err)
		}

		r.set(output)
	case "strftime":
		out, limit, format, tm := r.arg(0), r.arg(1), r.cstring(r.arg(2)), r.arg(3)
		if format != "%a, %d %b %Y %H:%M:%S GMT" {
			panic("unsupported date output format " + format)
		}

		data, err := r.engine.MemRead(tm, 36)
		if err != nil {
			panic(err)
		}

		field := func(i int) int { return int(int32(binary.LittleEndian.Uint32(data[i*4:]))) }

		value := time.Date(field(5)+1900, time.Month(field(4)+1), field(3), field(2), field(1), field(0), 0, time.UTC).Format("Mon, 02 Jan 2006 15:04:05 GMT")
		if uint64(len(value))+1 > limit {
			r.set(0)

			return
		}

		if err = r.MemWrite(out, append([]byte(value), 0)); err != nil {
			panic(err)
		}

		r.set(uint64(len(value)))
	case "strptime":
		value, format, out := r.cstring(r.arg(0)), r.cstring(r.arg(1)), r.arg(2)
		if format != "%a, %d %b %Y %H:%M:%S GMT" && format != "%a, %d %b %Y %H:%M:%S %Z" {
			panic("unsupported date format " + format)
		}

		date, err := time.Parse("Mon, 02 Jan 2006 15:04:05 GMT", value)
		if err != nil {
			r.set(0)

			return
		}

		fields := []int{date.Second(), date.Minute(), date.Hour(), date.Day(), int(date.Month()) - 1, date.Year() - 1900, int(date.Weekday()), date.YearDay() - 1, 0}
		for i, value := range fields {
			var word [4]byte

			binary.LittleEndian.PutUint32(word[:], uint32(value))

			if err = r.MemWrite(out+uint64(i*4), word[:]); err != nil {
				panic(err)
			}
		}

		r.set(r.arg(0) + uint64(len(value)))
	case "timegm":
		data, err := r.engine.MemRead(r.arg(0), 36)
		if err != nil {
			panic(err)
		}

		field := func(i int) int { return int(int32(binary.LittleEndian.Uint32(data[i*4:]))) }
		r.set(uint64(time.Date(field(5)+1900, time.Month(field(4)+1), field(3), field(2), field(1), field(0), 0, time.UTC).Unix()))
	case "CFURLResponseCopyPeerAddress":
		// The portable response exposes no CFNetwork peer-address metadata.
		r.set(0)
	case "res_9_ndestroy":
		// No native resolver state was created by the failed res_9_ninit.
		return
	case "res_9_ninit":
		// CFNetwork DNS diagnostics are unavailable; the Go transport owns DNS.
		r.set(uint64(0xffffffff))
	case "CRIsAutoSubmitEnabled":
		// The isolated runtime does not opt into diagnostics submission.
		r.set(0)
	case "NSUserName", "NSFullUserName":
		r.set(r.New("NSString", "ipatool"))
	case "NSTemporaryDirectory":
		r.set(r.New("NSString", "/ipatool/tmp/"))
	case "CPSharedResourcesDirectory", "NSHomeDirectory":
		r.set(r.New("NSString", "/ipatool"))
	case "getuid", "geteuid":
		r.set(501)
	case "getgid", "getegid":
		r.set(20)
	case "NSSearchPathForDirectoriesInDomains":
		if r.arg(0) != 5 || r.arg(1) != 8 {
			panic("unsupported virtual filesystem directory")
		}

		r.set(r.New("NSArray", []uint64{r.New("NSString", "/System/Library")}))

	case "MGCopyAnswerWithError", "MGCopyAnswer":
		key := r.String(r.arg(0))

		var value string

		switch key {
		case "BuildVersion":
			value = "26A434"
		case "ProductVersion":
			value = "27.0.1"
		case "DeviceClass":
			value = "Mac"
		case "ProductType":
			value = "Mac14,2"
		default:
			panic("unsupported device property " + key)
		}

		if name == "MGCopyAnswerWithError" && r.arg(2) != 0 {
			if err := r.MemWrite(r.arg(2), []byte{0, 0, 0, 0}); err != nil {
				panic(err)
			}
		}

		r.set(r.New("NSString", value))

	case "time":
		now := uint64(time.Now().Unix())
		if r.arg(0) != 0 {
			r.write64(r.arg(0), now)
		}

		r.set(now)
	case "localtime", "localtime_r", "gmtime_r":
		now := time.Unix(int64(r.read64(r.arg(0))), 0).UTC()

		out := r.arg(1)
		if name == "localtime" {
			out = r.alloc(56)
		}

		data := make([]byte, 56)
		fields := []int{now.Second(), now.Minute(), now.Hour(), now.Day(), int(now.Month()) - 1, now.Year() - 1900, int(now.Weekday()), now.YearDay() - 1, 0}

		for i, v := range fields {
			binary.LittleEndian.PutUint32(data[i*4:], uint32(v))
		}

		if err := r.MemWrite(out, data); err != nil {
			panic(err)
		}

		r.set(out)

	case "objc_getAssociatedObject":
		r.set(r.associations[r.arg(0)][r.arg(1)])
	case "objc_setAssociatedObject":
		object, key, value, policy := r.arg(0), r.arg(1), r.arg(2), r.arg(3)

		if r.associations == nil {
			r.associations = map[uint64]map[uint64]uint64{}
		}

		if r.associations[object] == nil {
			r.associations[object] = map[uint64]uint64{}
		}

		save := func(value uint64) { r.associations[object][key] = value; r.set(0) }
		if policy&255 == 3 {
			r.copyValue(value, save)
		} else {
			save(value)
		}

	case "CFPreferencesGetAppIntegerValue":
		p := r.preferences[r.String(r.arg(1))+"/"+r.String(r.arg(0))]

		var value int64

		valid := false
		if object := r.objects[p]; object != nil {
			value, valid = object.Value.(int64)
		}

		if out := r.arg(2); out != 0 {
			if err := r.MemWrite(out, []byte{byte(boolValue(valid))}); err != nil {
				panic(err)
			}
		}

		r.set(uint64(value))
	case "CFPreferencesGetAppBooleanValue":
		p := r.preferences[r.String(r.arg(1))+"/"+r.String(r.arg(0))]
		value, valid := false, false

		if obj := r.objects[p]; obj != nil {
			switch v := obj.Value.(type) {
			case bool:
				value, valid = v, true
			case int64:
				value, valid = v != 0, true
			}
		}

		if out := r.arg(2); out != 0 {
			if err := r.MemWrite(out, []byte{byte(boolValue(valid))}); err != nil {
				panic(err)
			}
		}

		r.set(boolValue(value))
	case "CFPreferencesAppSynchronize", "CFPreferencesSynchronize":
		r.set(1)
	case "CFPreferencesCopyAppValue", "CFPreferencesCopyValue":
		r.set(r.preferences[r.String(r.arg(1))+"/"+r.String(r.arg(0))])
	case "CFPreferencesSetAppValue", "CFPreferencesSetValue":
		if r.preferences == nil {
			r.preferences = map[string]uint64{}
		}

		r.preferences[r.String(r.arg(2))+"/"+r.String(r.arg(0))] = r.arg(1)
		r.set(0)

	case "objc_opt_self", "objc_unsafeClaimAutoreleasedReturnValue", "objc_retain", "objc_retainAutoreleasedReturnValue", "objc_claimAutoreleasedReturnValue", "objc_autoreleaseReturnValue", "objc_autorelease", "objc_retainAutoreleaseReturnValue", "objc_retainAutorelease":
		return
	case "objc_release", "objc_autoreleasePoolPop":
		return
	case "objc_autoreleasePoolPush":
		r.set(0)
	case "objc_getProperty":
		r.set(r.read64(r.arg(0) + r.arg(2)))
	case "objc_setProperty_nonatomic", "objc_setProperty_atomic":
		r.write64(r.arg(0)+r.arg(3), r.arg(2))
	case "objc_setProperty_nonatomic_copy", "objc_setProperty_atomic_copy":
		destination, value := r.arg(0)+r.arg(3), r.arg(2)
		r.copyValue(value, func(copied uint64) { r.write64(destination, copied); r.set(0) })

	case "objc_storeStrong", "objc_storeWeak", "objc_initWeak":
		r.write64(r.arg(0), r.arg(1))
		r.set(r.arg(1))
	case "objc_copyWeak":
		r.write64(r.arg(0), r.read64(r.arg(1)))
	case "objc_moveWeak":
		r.write64(r.arg(0), r.read64(r.arg(1)))
		r.write64(r.arg(1), 0)
	case "objc_loadWeakRetained", "objc_loadWeak":
		r.set(r.read64(r.arg(0)))
	case "objc_destroyWeak":
		r.write64(r.arg(0), 0)
	case "objc_alloc", "objc_allocWithZone", "objc_alloc_init", "objc_opt_new":
		class := r.classNames[r.arg(0)]
		if class == "" {
			panic(fmt.Sprintf("unknown allocated class %#x", r.arg(0)))
		}

		obj := r.New(class, nil)
		r.set(obj)

		if name != "objc_alloc" && name != "objc_allocWithZone" {
			r.message(obj, "init", false)
		}
	case "objc_opt_class":
		if _, ok := r.classNames[r.arg(0)]; ok {
			return
		}

		obj := r.objects[r.arg(0)]
		if obj == nil {
			panic(fmt.Sprintf("unknown object class %#x", r.arg(0)))
		}

		r.set(r.classes[obj.Class].Address)
	case "objc_opt_isKindOfClass":
		object, target := r.objects[r.arg(0)], r.classNames[r.arg(1)]
		if object == nil {
			r.set(0)

			return
		}

		r.set(boolValue(r.kindOf(object.Class, target)))
	case "objc_opt_respondsToSelector":
		r.set(boolValue(r.responds(r.arg(0), r.cstring(r.arg(1)))))

	case "objc_msgSend":
		r.message(r.arg(0), r.cstring(r.arg(1)), false)
	case "objc_msgSendSuper2":
		super := r.arg(0)
		receiver := r.read64(super)
		class := r.classNames[r.read64(super+8)]
		selector := r.cstring(r.arg(1))
		_, meta := r.classNames[receiver]

		if c := r.classes[class]; c != nil {
			if entry := r.lookup(c.Super, selector, meta); entry != 0 {
				r.jump(entry, receiver, selector)

				return
			}
		}

		r.message(receiver, selector, true)
	case "CFBundleGetMainBundle":
		r.set(r.bundle())
	case "CFBundleGetInfoDictionary":
		r.set(r.bundleInfo())
	case "CFBundleGetValueForInfoDictionaryKey":
		info := r.objects[r.bundleInfo()].Value.(map[string]uint64)
		r.set(info[r.String(r.arg(1))])
	case "CFBundleCopyExecutableURL":
		r.set(r.New("NSURL", "file:///ipatool"))
	case "getprogname":
		v := []byte("ipatool\x00")
		ptr := r.alloc(uint64(len(v)))
		_ = r.MemWrite(ptr, v)
		r.set(ptr)
	case "getpid", "_getpid":
		r.set(123)
	case "CFGetTypeID":
		obj := r.objects[r.arg(0)]
		if obj == nil {
			panic("CFGetTypeID of unknown object")
		}

		switch obj.Value.(type) {
		case bool:
			r.set(4)
		case []byte:
			r.set(1)
		case string:
			r.set(2)
		default:
			r.set(3)
		}
	case "CFBooleanGetTypeID":
		r.set(4)
	case "CFBooleanGetValue":
		r.set(boolValue(r.objects[r.arg(0)].Value.(bool)))
	case "CFDataGetTypeID":
		r.set(1)
	case "CFStringGetTypeID":
		r.set(2)
	case "IOMasterPort":
		r.write32(r.arg(1), 1)
		r.set(0)
	case "IOServiceMatching":
		r.set(r.New("NSDictionary", map[string]uint64{"IOProviderClass": r.New("NSString", r.cstring(r.arg(0)))}))
	case "IOBSDNameMatching":
		r.set(r.New("NSDictionary", map[string]uint64{"IOBSDName": r.New("NSString", r.cstring(r.arg(2)))}))
	case "IOServiceGetMatchingService":
		r.set(0) // The virtual device exposes no platform expert/serial service.
	case "IOServiceGetMatchingServices":
		r.write32(r.arg(2), 1)
		r.set(0)
	case "IOIteratorNext":
		r.set(1)
	case "IORegistryEntryGetParentEntry":
		r.write32(r.arg(2), 2)
		r.set(0)
	case "IORegistryEntryCreateCFProperty", "IORegistryEntrySearchCFProperty":
		if r.arg(0) == 0 {
			r.set(0)

			return
		}

		keyIndex := 1
		if name == "IORegistryEntrySearchCFProperty" {
			keyIndex = 2
		}

		key := r.String(r.arg(keyIndex))

		switch key {
		case "IOBuiltin", "IOPrimaryInterface":
			r.set(r.New("NSNumber", true))
		case "IOMACAddress":
			r.set(r.New("NSData", append([]byte(nil), r.hardwareID...)))
		default:
			panic("unsupported IOKit property " + key)
		}
	case "IOObjectRelease", "CFRelease":
		r.set(0)
	case "CFDataGetLength":
		r.set(uint64(len(r.objects[r.arg(0)].Value.([]byte))))
	case "CFDataGetBytePtr":
		data := r.objects[r.arg(0)].Value.([]byte)
		ptr := r.alloc(uint64(len(data)))
		_ = r.MemWrite(ptr, data)
		r.set(ptr)
	case "CFDataGetBytes":
		data := r.objects[r.arg(0)].Value.([]byte)
		start, length, dst := r.arg(1), r.arg(2), r.arg(3)
		_ = r.MemWrite(dst, data[start:start+length])
		r.set(0)
	case "CFUUIDCreate":
		data := make([]byte, 16)
		if _, err := rand.Read(data); err != nil {
			panic(err)
		}

		data[6] = (data[6] & 15) | 64
		data[8] = (data[8] & 63) | 128
		r.set(r.New("HostUUID", data))
	case "CFUUIDGetUUIDBytes":
		data := r.objects[r.arg(0)].Value.([]byte)
		r.set(binary.LittleEndian.Uint64(data))
		_ = r.engine.RegWrite(unicorn.RegX1, binary.LittleEndian.Uint64(data[8:]))
	case "NSClassFromString":
		r.set(r.ClassAddress(r.String(r.arg(0))))
	case "class_getMethodImplementation":
		r.set(r.lookup(r.classNames[r.arg(0)], r.cstring(r.arg(1)), false))
	case "sel_getName":
		return
	case "objc_getClass":
		r.set(r.ClassAddress(r.cstring(r.arg(0))))
	case "NSStringFromClass":
		if r.arg(0) == 0 {
			r.set(0)
		} else {
			r.set(r.New("NSString", r.classNames[r.arg(0)]))
		}
	case "NSSelectorFromString":
		r.set(r.selector(r.String(r.arg(0))))
	case "NSStringFromSelector":
		r.set(r.New("NSString", r.cstring(r.arg(0))))
	case "SecTaskCreateFromSelf":
		r.set(r.New("HostSecurityTask", nil))
	case "SecTaskCopyValueForEntitlement":
		if out := r.arg(2); out != 0 {
			r.write64(out, 0)
		}

		r.set(0)
	case "dispatch_time":
		base := r.arg(0)
		if base == 0 {
			base = uint64(time.Now().UnixNano())
		}

		r.set(base + r.arg(1))
	case "dispatch_after":
		deadline, block := r.arg(0), r.arg(2)
		r.copyBlock(block, func(copied uint64) { r.timers = append(r.timers, timer{at: deadline, block: copied}); r.set(0) })
	case "dispatch_assert_queue":
		if !r.queueIsWithin(r.currentQueue, r.arg(0)) {
			panic(fmt.Sprintf("dispatch queue mismatch: running %#x expected %#x", r.currentQueue, r.arg(0)))
		}
	case "dispatch_sync":
		previous := r.currentQueue
		r.currentQueue = r.arg(0)
		r.callback(r.arg(1), nil, func() { r.currentQueue = previous; r.set(0) })
	case "dispatch_sync_f":
		previous := r.currentQueue
		r.currentQueue = r.arg(0)
		r.callbackFunction(r.arg(2), []uint64{r.arg(1)}, func() { r.currentQueue = previous; r.set(0) })
	case "dispatch_block_create":
		r.copyBlock(r.arg(1), func(block uint64) { r.set(block) })
	case "dispatch_async":
		queue := r.arg(0)
		r.copyBlock(r.arg(1), func(block uint64) { r.queuePending(block, queue); r.set(0) })
	case "dispatch_suspend":
		r.objects[r.arg(0)].Properties["suspendCount"]++
	case "dispatch_resume":
		q := r.objects[r.arg(0)]
		if q.Properties["suspendCount"] == 0 {
			panic("resuming active dispatch queue")
		}

		q.Properties["suspendCount"]--
	case "dispatch_retain", "dispatch_release":
		return
	case "dispatch_semaphore_create":
		r.set(r.New("HostSemaphore", int64(r.arg(0))))
	case "dispatch_semaphore_signal":
		obj := r.objects[r.arg(0)]
		obj.Value = obj.Value.(int64) + 1

		r.set(0)
	case "dispatch_semaphore_wait":
		semaphore := r.objects[r.arg(0)]
		if semaphore.Value.(int64) > 0 {
			semaphore.Value = semaphore.Value.(int64) - 1

			r.set(0)
		} else if len(r.pending) > 0 {
			block := r.nextPending()
			if block == 0 {
				panic("all pending queues are suspended")
			}

			r.pendingCallback(block, func() {
				if semaphore.Value.(int64) == 0 {
					panic("guest semaphore remains unsignaled")
				}

				semaphore.Value = semaphore.Value.(int64) - 1

				r.set(0)
			})
		} else {
			panic("guest waits on an unavailable host service")
		}
	case "Block_copy", "objc_retainBlock":
		r.copyBlock(r.arg(0), func(value uint64) { r.set(value) })
	case "Block_release", "Block_object_dispose":
		return
	case "Block_object_assign":
		destination, value, flags := r.arg(0), r.arg(1), r.arg(2)
		if flags&0xf == 7 {
			r.copyBlock(value, func(copied uint64) { r.write64(destination, copied) })
		} else if flags&8 != 0 {
			r.copyByref(value, func(copied uint64) { r.write64(destination, copied) })
		} else {
			r.write64(destination, value)
		}
	case "dispatch_once":
		predicate, block := r.arg(0), r.arg(1)
		if r.read64(predicate) != 0 {
			r.set(0)

			return
		}

		r.write64(predicate, ^uint64(0))
		r.pendingCallback(block, func() { r.set(0) })
	case "swift_once":
		predicate, function, context := r.arg(0), r.arg(1), r.arg(2)
		if r.read64(predicate) != 0 {
			r.set(0)

			return
		}

		r.write64(predicate, ^uint64(0))
		r.callbackFunction(function, []uint64{context}, func() { r.set(0) })
	case "dispatch_once_f":
		predicate, context, function := r.arg(0), r.arg(1), r.arg(2)
		if r.read64(predicate) != 0 {
			r.set(0)

			return
		}

		r.write64(predicate, ^uint64(0))
		r.callbackFunction(function, []uint64{context}, func() { r.set(0) })
	case "dispatch_queue_attr_make_with_autorelease_frequency", "dispatch_queue_attr_make_with_qos_class":
		return
	case "dispatch_queue_create_with_target":
		target := r.arg(2)
		queue := r.New("HostQueue", nil)
		r.objects[queue].Properties["targetQueue"] = target
		r.set(queue)
	case "dispatch_set_target_queue":
		r.objects[r.arg(0)].Properties["targetQueue"] = r.arg(1)
	case "dispatch_queue_create", "dispatch_get_global_queue", "dispatch_get_main_queue":
		r.set(r.New("HostQueue", nil))
	case "os_unfair_lock_assert_not_owner", "objc_sync_enter", "objc_sync_exit", "os_unfair_lock_lock_with_options", "os_unfair_lock_lock", "os_unfair_lock_unlock", "os_unfair_lock_assert_owner", "os_unfair_recursive_lock_lock_with_options", "os_unfair_recursive_lock_unlock":
		r.set(0)
	case "os_feature_enabled_impl":
		key := r.cstring(r.arg(0)) + "/" + r.cstring(r.arg(1))
		value, ok := r.profile.Features[key]

		if !ok {
			panic("unresolved native feature flag " + key)
		}

		r.set(boolValue(value))
	case "os_log_create":
		r.set(r.New("HostOSLog", nil))
	case "os_signpost_enabled", "os_log_type_enabled", "os_variant_has_internal_content":
		r.set(0)
	case "malloc_size":
		r.set(r.allocations[r.arg(0)])
	case "malloc", "calloc", "malloc_type_malloc", "malloc_type_calloc":
		size := r.arg(0)
		if name == "calloc" || name == "malloc_type_calloc" {
			if size != 0 && r.arg(1) > heapSize/size {
				panic("guest calloc exceeds heap budget")
			}

			size *= r.arg(1)
		}

		r.set(r.alloc(size))
	case "chkstk_darwin", "free":
		return
	case "platform_memmove", "platform_memcpy", "memcpy", "memmove", "memcpy_chk", "memmove_chk":
		if r.arg(2) == 0 {
			return
		}

		if r.arg(2) > heapSize {
			panic("guest copy exceeds memory budget")
		}

		data, e := r.engine.MemRead(r.arg(1), r.arg(2))
		if e != nil {
			panic(e)
		}

		if e = r.MemWrite(r.arg(0), data); e != nil {
			panic(e)
		}
	case "memset", "memset_chk":
		if r.arg(2) > heapSize {
			panic("guest fill exceeds memory budget")
		}

		data := make([]byte, r.arg(2))
		for i := range data {
			data[i] = byte(r.arg(1))
		}

		if e := r.MemWrite(r.arg(0), data); e != nil {
			panic(e)
		}
	case "strlen":
		r.set(uint64(len(r.cstring(r.arg(0)))))
	default:
		if r.swiftSymbol(name) {
			return
		}

		lr, _ := r.engine.RegRead(unicorn.RegLR)
		panic(fmt.Sprintf("unsupported import %s lr=%#x", name, lr))
	}
}
func boolValue(v bool) uint64 {
	if v {
		return 1
	}

	return 0
}
func (r *Runtime) kindOf(class, target string) bool {
	if class == target {
		return true
	}

	if target == "NSObject" {
		return true
	}

	for class != "" {
		c := r.classes[class]
		if c == nil {
			return false
		}

		if c.Super == target {
			return true
		}

		class = c.Super
	}

	return false
}
func (r *Runtime) message(receiver uint64, selector string, super bool) {
	_ = super
	if receiver == 0 {
		_ = r.engine.RegWrite(unicorn.RegX1, 0)
		for register := 40; register < 44; register++ {
			_ = r.engine.RegWrite(register, 0)
		}

		r.set(0)

		return
	}

	class, meta := r.classNames[receiver]
	object := r.objects[receiver]

	if !meta {
		if object == nil {
			if selector == "copy" && receiver >= heapBase && receiver < stackBase+stackSize {
				r.copyBlock(receiver, func(copied uint64) { r.set(copied) })

				return
			}

			panic(fmt.Sprintf("unknown receiver %#x for %s", receiver, selector))
		}

		class = object.Class
	}

	if class == "HostUnavailableProxy" {
		domain := r.New("NSString", "NSCocoaErrorDomain")
		err := r.New("NSError", map[string]uint64{"domain": domain, "code": r.New("NSNumber", int64(4099)), "userInfo": r.New("NSDictionary", map[string]uint64{})})
		r.callback(object.Properties["errorBlock"], []uint64{err}, func() { r.set(0) })

		return
	}
	// Logging is a host service; it must not pull platform logging infrastructure into the guest.
	if class == "AMSLogConfig" || (class == "AMSConcurrency" && selector == "logFaultFromSwiftConcurrencyOnInternalBuild") {
		r.set(0)

		return
	}

	if entry := r.lookup(class, selector, meta); !super && entry != 0 {
		r.jump(entry, receiver, selector)

		return
	}

	if r.notificationMessage(receiver, class, selector, meta) {
		return
	}

	if r.accountMessage(receiver, class, selector, meta) {
		return
	}

	if r.networkMessage(receiver, class, selector, meta) {
		return
	}

	if !meta && (class == "NSURLSessionConfiguration" || class == "NSOperationQueue" || class == "NSCache") {
		if strings.HasPrefix(selector, "set") && strings.HasSuffix(selector, ":") && strings.Count(selector, ":") == 1 {
			key := selector[3 : len(selector)-1]
			if len(key) < 2 || key[1] < 'A' || key[1] > 'Z' {
				key = strings.ToLower(key[:1]) + key[1:]
			}

			object.Properties[key] = r.arg(2)
			r.set(0)

			return
		}

		if value, ok := object.Properties[selector]; ok {
			r.set(value)

			return
		}
	}

	if selector == "initWithObjectsAndKeys:" || (meta && selector == "dictionaryWithObjectsAndKeys:") {
		values := map[string]uint64{}
		v := r.arg(2)
		sp, _ := r.engine.RegRead(unicorn.RegSP)

		for index := uint64(0); v != 0; index++ {
			if index > 1024 {
				panic("dictionary varargs exceeds budget")
			}

			key := r.read64(sp + index*16)
			values[r.collectionKey(key)] = v
			v = r.read64(sp + index*16 + 8)
		}

		if meta {
			r.set(r.New(class, values))
		} else {
			object.Value = values

			r.set(receiver)
		}

		return
	}

	if meta {
		switch selector {
		case "instanceMethodForSelector:":
			r.set(r.lookup(class, r.cstring(r.arg(2)), false))

			return
		case "alloc", "allocWithZone:":
			r.set(r.New(class, nil))

			return
		case "new":
			obj := r.New(class, nil)
			r.set(obj)
			r.message(obj, "init", false)

			return
		case "isSubclassOfClass:":
			r.set(boolValue(r.kindOf(class, r.classNames[r.arg(2)])))

			return
		case "class":
			r.set(receiver)

			return
		case "setWithObjects:", "arrayWithObjects:":
			items := []uint64{}
			if first := r.arg(2); first != 0 {
				items = append(items, first)

				sp, _ := r.engine.RegRead(unicorn.RegSP)
				for i := uint64(0); i < 1024; i++ {
					v := r.read64(sp + i*8)
					if v == 0 {
						break
					}

					items = append(items, v)
				}
			}

			r.set(r.New(class, items))

			return
		case "dictionaryWithDictionary:":
			d := map[string]uint64{}
			for k, v := range r.objects[r.arg(2)].Value.(map[string]uint64) {
				d[k] = v
			}

			r.set(r.New(class, d))

			return
		case "fileURLWithPath:", "fileURLWithPath:isDirectory:":
			r.set(r.New("NSURL", (&url.URL{Scheme: "file", Path: r.String(r.arg(2))}).String()))

			return
		case "URLWithString:":
			r.set(r.New("NSURL", r.String(r.arg(2))))

			return
		case "setWithObject:", "arrayWithObject:":
			r.set(r.New(class, []uint64{r.arg(2)}))

			return
		case "setWithArray:":
			var items []uint64
			if r.arg(2) != 0 {
				items = append(items, r.objects[r.arg(2)].Value.([]uint64)...)
			}

			r.set(r.New(class, items))

			return
		case "UUID":
			r.set(r.New("NSUUID", r.newUUIDString()))

			return
		case "distantPast":
			r.set(r.New("NSDate", time.Unix(-62135769600, 0).UTC()))

			return
		case "distantFuture":
			r.set(r.New("NSDate", time.Date(4001, 1, 1, 0, 0, 0, 0, time.UTC)))

			return
		case "dateWithTimeIntervalSinceNow:", "dateWithTimeIntervalSince1970:":
			bits, _ := r.engine.RegRead(40)

			seconds := math.Float64frombits(bits)
			if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
				panic("unsupported date interval")
			}

			if selector == "dateWithTimeIntervalSinceNow:" {
				now := time.Now()
				seconds += float64(now.Unix()) + float64(now.Nanosecond())/1e9
			}

			if seconds <= math.MinInt64 || seconds >= math.MaxInt64 {
				panic("date interval exceeds supported range")
			}

			whole, fraction := math.Modf(seconds)
			r.set(r.New("NSDate", time.Unix(int64(whole), int64(fraction*1e9)).UTC()))

			return
		case "date", "now":
			r.set(r.New("NSDate", time.Now()))

			return
		case "defaultSessionConfiguration", "ephemeralSessionConfiguration":
			obj := r.New("NSURLSessionConfiguration", nil)
			r.objects[obj].Properties = map[string]uint64{"usesClassicLoadingMode": 1, "protocolClasses": r.New("NSArray", []uint64{}), "HTTPShouldSetCookies": 1, "HTTPMaximumConnectionsPerHost": 6}
			r.set(obj)

			return
		case "interfaceWithProtocol:":
			r.set(r.New("NSXPCInterface", nil))

			return
		case "currentThread":
			r.set(r.New("NSThread", nil))

			return
		case "isMainThread":
			r.set(1)

			return
		case "null":
			if r.nullObject == 0 {
				r.nullObject = r.New("NSNull", nil)
			}

			r.set(r.nullObject)

			return
		case "standardUserDefaults":
			r.set(r.New("NSUserDefaults", map[string]uint64{}))

			return
		case "preferredLanguages":
			r.set(r.New("NSArray", []uint64{r.New("NSString", "en-US")}))

			return
		case "decimalDigitCharacterSet":
			r.set(r.New("NSCharacterSet", "0123456789"))

			return
		case "characterSetWithCharactersInString:":
			r.set(r.New("NSCharacterSet", r.String(r.arg(2))))

			return
		case "currentLocale", "systemLocale":
			r.set(r.New("NSLocale", "en_US"))

			return
		case "defaultManager":
			r.set(r.New("NSFileManager", nil))

			return
		case "bundleWithPath:":
			value := r.String(r.arg(2))
			if value != "/System/Library/Frameworks/WebKit.framework" {
				r.set(0)

				return
			}

			bundle := r.New("NSBundle", nil)
			r.objects[bundle].Properties = map[string]uint64{"bundlePath": r.arg(2), "infoDictionary": r.New("NSDictionary", map[string]uint64{"CFBundleVersion": r.New("NSString", "22625.1.29.11.28")})}
			r.set(bundle)

			return
		case "bundleForClass:":
			bundle := r.bundle()

			if strings.Contains(r.classNames[r.arg(2)], "AMS") {
				r.objects[bundle].Properties["bundleIdentifier"] = r.New("NSString", "com.apple.AppleMediaServices")
			}

			r.set(bundle)

			return
		case "mainBundle":
			r.set(r.bundle())

			return

		case "processInfo":
			obj := r.New("NSProcessInfo", nil)
			r.objects[obj].Properties = map[string]uint64{"processName": r.New("NSString", "ipatool"), "environment": r.New("NSDictionary", map[string]uint64{})}
			r.set(obj)

			return
		case "dictionary", "dictionaryWithCapacity:":
			r.set(r.New(class, map[string]uint64{}))

			return
		case "weakToStrongObjectsMapTable", "weakToWeakObjectsMapTable", "strongToWeakObjectsMapTable", "strongToStrongObjectsMapTable":
			r.set(r.New("NSMapTable", map[string]uint64{}))

			return
		case "set", "setWithCapacity:", "array", "arrayWithCapacity:":
			r.set(r.New(class, []uint64{}))

			return
		case "numberWithDouble:", "numberWithFloat:":
			bits, _ := r.engine.RegRead(40)
			value := math.Float64frombits(bits)

			if selector == "numberWithFloat:" {
				value = float64(math.Float32frombits(uint32(bits)))
			}

			r.set(r.New("NSNumber", value))

			return
		case "numberWithBool:":
			r.set(r.New("NSNumber", r.arg(2) != 0))

			return
		case "numberWithInteger:", "numberWithInt:", "numberWithUnsignedInteger:", "numberWithLongLong:", "numberWithUnsignedLongLong:":
			r.set(r.New("NSNumber", int64(r.arg(2))))

			return
		case "dictionaryWithObjects:forKeys:count:":
			values, keys, count := r.arg(2), r.arg(3), r.arg(4)
			if count > 65536 {
				panic("guest dictionary exceeds runtime limit")
			}

			d := map[string]uint64{}
			for i := uint64(0); i < count; i++ {
				d[r.String(r.read64(keys+i*8))] = r.read64(values + i*8)
			}

			r.set(r.New("NSDictionary", d))

			return
		case "setWithObjects:count:", "arrayWithObjects:count:":
			values, count := r.arg(2), r.arg(3)
			if count > 65536 {
				panic("guest array exceeds runtime limit")
			}

			a := make([]uint64, count)
			for i := range a {
				a[i] = r.read64(values + uint64(i)*8)
			}

			r.set(r.New(class, a))

			return
		case "stringWithFormat:":
			r.set(r.New("NSString", r.format(r.String(r.arg(2)))))

			return
		case "stringWithUTF8String:":
			r.set(r.New("NSString", r.cstring(r.arg(2))))

			return
		case "errorWithDomain:code:userInfo:":
			r.set(r.New("NSError", map[string]uint64{"domain": r.arg(2), "code": r.New("NSNumber", int64(r.arg(3))), "userInfo": r.arg(4)}))

			return
		default:
			panic("unsupported class message " + methodKey(class, selector, true))
		}
	}

	switch selector {
	case "initWithMachServiceName:options:":
		object.Properties["serviceName"] = r.arg(2)
		r.set(receiver)
	case "_setQueue:":
		object.Properties["queue"] = r.arg(2)
		r.set(0)
	case "setRemoteObjectInterface:":
		object.Properties["interface"] = r.arg(2)
		r.set(0)
	case "setInterruptionHandler:", "setInvalidationHandler:":
		r.copyBlock(r.arg(2), func(block uint64) { object.Properties[selector] = block; r.set(0) })
	case "remoteObjectProxyWithErrorHandler:":
		r.copyBlock(r.arg(2), func(block uint64) {
			proxy := r.New("HostUnavailableProxy", nil)
			r.objects[proxy].Properties["errorBlock"] = block
			r.set(proxy)
		})
	case "resume", "invalidate", "setClass:forSelector:argumentIndex:ofReply:", "setClasses:forSelector:argumentIndex:ofReply:", "setInterface:forSelector:argumentIndex:ofReply:":
		if class != "NSXPCConnection" && class != "NSXPCInterface" {
			panic("unsupported message " + class + " " + selector)
		}

		r.set(0)
	case "addOperationWithBlock:":
		r.copyBlock(r.arg(2), func(block uint64) { r.queuePending(block, receiver); r.set(0) })
	case "lock", "unlock", "signal", "broadcast":
		if class != "NSCondition" && class != "NSLock" && class != "NSRecursiveLock" {
			panic("unsupported lock receiver " + class)
		}

		r.set(0)
	case "wait", "waitUntilDate:":
		if class != "NSCondition" || len(r.pending) == 0 {
			panic("guest condition has no runnable callback")
		}

		block := r.nextPending()
		if block == 0 {
			panic("all pending queues are suspended")
		}

		r.pendingCallback(block, func() { r.set(1) })
	case "createDirectoryAtURL:withIntermediateDirectories:attributes:error:", "createDirectoryAtPath:withIntermediateDirectories:attributes:error:":
		if r.arg(5) != 0 {
			r.write64(r.arg(5), r.New("NSError", map[string]uint64{"domain": r.New("NSString", "NSCocoaErrorDomain"), "code": r.New("NSNumber", int64(513)), "userInfo": r.New("NSDictionary", map[string]uint64{})}))
		}

		r.set(0)
	case "isWritableFileAtPath:", "fileExistsAtPath:", "fileExistsAtPath:isDirectory:":
		if selector == "fileExistsAtPath:isDirectory:" && r.arg(3) != 0 {
			_ = r.MemWrite(r.arg(3), []byte{0})
		}

		r.set(0)
	case "URLsForDirectory:inDomains:":
		if class != "NSFileManager" {
			panic("invalid file-manager receiver")
		}

		r.set(r.New("NSArray", []uint64{r.New("NSURL", "file:///ipatool/cache/")}))
	case "URLForDirectory:inDomain:appropriateForURL:create:error:":
		r.set(r.New("NSURL", "file:///ipatool/cache/"))
	case "threadDictionary":
		if object.Properties[selector] == 0 {
			object.Properties[selector] = r.New("NSMutableDictionary", map[string]uint64{})
		}

		r.set(object.Properties[selector])
	case "boolForKey:":
		value := object.Value.(map[string]uint64)[r.String(r.arg(2))]
		if value == 0 {
			r.set(0)
		} else {
			r.message(value, "boolValue", false)
		}
	case "operatingSystemVersion":
		out, _ := r.engine.RegRead(unicorn.RegX0 + 8)
		r.write64(out, 27)
		r.write64(out+8, 0)
		r.write64(out+16, 1)
	case "operatingSystemVersionString":
		r.set(r.New("NSString", "Version 27.0.1 (Build 26A434)"))
	case "initWithTimeIntervalSince1970:":
		if class != "NSDate" {
			panic("unsupported date initializer")
		}

		bits, _ := r.engine.RegRead(40)
		seconds, fraction := math.Modf(math.Float64frombits(bits))
		object.Value = time.Unix(int64(seconds), int64(fraction*1e9)).UTC()

		r.set(receiver)
	case "initWithBytesNoCopy:length:encoding:freeWhenDone:":
		if !r.kindOf(class, "NSString") || (r.arg(4) != 4 && r.arg(4) != 1) || r.arg(3) > 1<<20 {
			panic("unsupported string buffer initialization")
		}

		data, err := r.engine.MemRead(r.arg(2), r.arg(3))
		if err != nil {
			panic(err)
		}

		object.Value = string(data)

		r.set(receiver)
	case "timeIntervalSinceDate:":
		date := object.Value.(time.Time)
		other := r.objects[r.arg(2)].Value.(time.Time)
		_ = r.engine.RegWrite(40, math.Float64bits(date.Sub(other).Seconds()))
	case "timeIntervalSinceNow", "timeIntervalSince1970", "timeIntervalSinceReferenceDate":
		date := object.Value.(time.Time)
		seconds := float64(date.Unix()) + float64(date.Nanosecond())/1e9

		switch selector {
		case "timeIntervalSinceNow":
			seconds = time.Until(date).Seconds()
		case "timeIntervalSinceReferenceDate":
			seconds -= 978307200
		}

		_ = r.engine.RegWrite(40, math.Float64bits(seconds))
	case "localizedStringForKey:value:table:":
		if class != "NSBundle" {
			panic("unsupported localization receiver")
		}
		// No bundle resource files are mounted in this runtime. Foundation's
		// documented fallback is the supplied value, then the key itself.
		if value := r.arg(3); value != 0 && r.String(value) != "" {
			r.set(value)
		} else {
			r.set(r.arg(2))
		}
	case "localeIdentifier":
		r.set(r.New("NSString", "en_US"))
	case "languageCode":
		r.set(r.New("NSString", "en"))
	case "infoDictionary":
		if value := object.Properties["infoDictionary"]; value != 0 {
			r.set(value)
		} else {
			r.set(r.bundleInfo())
		}
	case "executableURL", "bundleURL":
		r.set(r.New("NSURL", "file:///ipatool"))
	case "bundleIdentifier", "bundlePath", "processName", "environment":
		r.set(object.Properties[selector])
	case "objectForInfoDictionaryKey:":
		p := object.Properties["infoDictionary"]
		if p == 0 {
			p = r.bundleInfo()
		}

		info := r.objects[p].Value.(map[string]uint64)
		r.set(info[r.String(r.arg(2))])
	case "initWithCapacity:":
		if class == "NSMutableString" {
			object.Value = ""
		} else if strings.Contains(class, "Array") {
			object.Value = []uint64{}
		} else {
			object.Value = map[string]uint64{}
		}

		r.set(receiver)
	case "appendFormat:":
		object.Value = r.String(receiver) + r.format(r.String(r.arg(2)))
		r.set(0)
	case "init":
		if object.Value == nil {
			switch class {
			case "NSString", "NSMutableString":
				object.Value = ""
			case "NSCache", "NSDictionary", "NSMutableDictionary":
				object.Value = map[string]uint64{}
			case "NSArray", "NSMutableArray", "NSSet", "NSMutableSet", "NSOrderedSet", "NSMutableOrderedSet":
				object.Value = []uint64{}
			}
		}

		r.set(receiver)
	case "containsObject:":
		items := object.Value.([]uint64)
		match := false

		for _, v := range items {
			match = match || v == r.arg(2) || (r.objects[v] != nil && r.objects[r.arg(2)] != nil && fmt.Sprint(r.objects[v].Value) == fmt.Sprint(r.objects[r.arg(2)].Value))
		}

		r.set(boolValue(match))
	case "UUIDString":
		r.set(r.New("NSString", object.Value))
	case "initWithFormat:":
		object.Value = r.format(r.String(r.arg(2)))
		r.set(receiver)
	case "initWithFormat:locale:arguments:":
		object.Value = r.formatAt(r.String(r.arg(2)), r.arg(4))
		r.set(receiver)
	case "initWithString:":
		object.Value = r.String(r.arg(2))
		r.set(receiver)
	case "stringByAppendingPathComponent:":
		r.set(r.New("NSString", path.Join(r.String(receiver), r.String(r.arg(2)))))
	case "stringByDeletingPathExtension":
		v := r.String(receiver)
		r.set(r.New("NSString", strings.TrimSuffix(v, path.Ext(v))))
	case "componentsJoinedByString:":
		items := object.Value.([]uint64)
		parts := make([]string, len(items))

		for i, v := range items {
			parts[i] = r.String(v)
		}

		r.set(r.New("NSString", strings.Join(parts, r.String(r.arg(2)))))
	case "componentsSeparatedByString:":
		parts := strings.Split(r.String(receiver), r.String(r.arg(2)))
		items := make([]uint64, len(parts))

		for i, v := range parts {
			items[i] = r.New("NSString", v)
		}

		r.set(r.New("NSArray", items))
	case "sortedArrayUsingComparator:":
		items := append([]uint64(nil), object.Value.([]uint64)...)
		if len(items) > 4096 {
			panic("sort exceeds runtime budget")
		}

		block := r.arg(2)
		i, j := 1, 1

		var next func()
		next = func() {
			if i >= len(items) {
				r.set(r.New("NSArray", items))

				return
			}

			if j == 0 {
				i++
				j = i

				next()

				return
			}

			r.callback(block, []uint64{items[j-1], items[j]}, func() {
				if int64(r.arg(0)) > 0 {
					items[j-1], items[j] = items[j], items[j-1]
					j--
				} else {
					i++
					j = i
				}

				next()
			})
		}
		next()
	case "compare:":
		if date, ok := object.Value.(time.Time); ok {
			other := r.objects[r.arg(2)].Value.(time.Time)
			r.set(uint64(int64(date.Compare(other))))

			return
		}

		r.set(uint64(strings.Compare(r.String(receiver), r.String(r.arg(2)))))
	case "allObjects":
		r.set(r.New("NSArray", append([]uint64(nil), object.Value.([]uint64)...)))
	case "firstObject", "lastObject":
		items := object.Value.([]uint64)
		if len(items) == 0 {
			r.set(0)
		} else if selector == "firstObject" {
			r.set(items[0])
		} else {
			r.set(items[len(items)-1])
		}
	case "objectAtIndex:", "objectAtIndexedSubscript:":
		r.set(object.Value.([]uint64)[r.arg(2)])
	case "removeObjectsInArray:", "removeObject:":
		if r.arg(2) == 0 {
			r.set(0)

			return
		}

		remove := []uint64{r.arg(2)}
		if selector == "removeObjectsInArray:" {
			remove = r.objects[r.arg(2)].Value.([]uint64)
		}

		items := []uint64{}

		for _, v := range object.Value.([]uint64) {
			found := false
			for _, q := range remove {
				found = found || r.foundationEqual(v, q)
			}

			if !found {
				items = append(items, v)
			}
		}

		object.Value = items

		r.set(0)
	case "addObjectsFromArray:":
		object.Value = append(object.Value.([]uint64), r.objects[r.arg(2)].Value.([]uint64)...)
		r.set(0)
	case "removeObjectAtIndex:":
		items := object.Value.([]uint64)
		i := r.arg(2)

		if i >= uint64(len(items)) {
			panic("array removal index out of bounds")
		}

		object.Value = append(items[:i], items[i+1:]...)

		r.set(0)
	case "addObject:":
		object.Value = append(object.Value.([]uint64), r.arg(2))
		r.set(0)
	case "path", "lastPathComponent":
		value := object.Value.(string)
		if class == "NSURL" {
			parsed, e := url.Parse(value)
			if e != nil {
				panic(e)
			}

			value = parsed.Path
		}

		if selector == "lastPathComponent" {
			value = path.Base(value)
		}

		r.set(r.New("NSString", value))
	case "absoluteString":
		r.set(r.New("NSString", object.Value.(string)))
	case "stringValue":
		r.set(r.New("NSString", fmt.Sprint(object.Value)))
	case "description":
		if !r.kindOf(class, "NSString") {
			panic("unsupported instance message " + methodKey(class, selector, false))
		}

		r.set(receiver)
	case "lowercaseString":
		r.set(r.New("NSString", strings.ToLower(r.String(receiver))))
	case "uppercaseString":
		r.set(r.New("NSString", strings.ToUpper(r.String(receiver))))
	case "appendString:":
		object.Value = r.String(receiver) + r.String(r.arg(2))
		r.set(0)
	case "stringByAppendingString:":
		r.set(r.New("NSString", r.String(receiver)+r.String(r.arg(2))))
	case "initWithDomain:code:userInfo:":
		object.Value = map[string]uint64{"domain": r.arg(2), "code": r.New("NSNumber", int64(r.arg(3))), "userInfo": r.arg(4)}
		r.set(receiver)
	case "copy":
		if entry := r.lookup(class, "copyWithZone:", false); entry != 0 {
			_ = r.engine.RegWrite(unicorn.RegX2, 0)
			r.jump(entry, receiver, "copyWithZone:")
		} else {
			r.set(r.foundationCopy(receiver, false))
		}
	case "retain", "autorelease", "copyWithZone:":
		r.set(receiver)
	case "mutableCopy", "mutableCopyWithZone:":
		if d, ok := object.Value.(map[string]uint64); ok {
			copied := map[string]uint64{}
			for k, v := range d {
				copied[k] = v
			}

			r.set(r.New("NSMutableDictionary", copied))
		} else {
			r.set(r.New(class, object.Value))
		}
	case "release":
		r.set(0)
	case "class":
		r.set(r.classes[class].Address)
	case "isKindOfClass:":
		r.set(boolValue(r.kindOf(class, r.classNames[r.arg(2)])))
	case "isEqualToDate:", "isEqual:", "isEqualToString:", "isEqualToDictionary:", "isEqualToArray:":
		r.set(boolValue(r.foundationEqual(receiver, r.arg(2))))
	case "length", "count":
		switch v := object.Value.(type) {
		case string:
			r.set(uint64(len(v)))
		case []byte:
			r.set(uint64(len(v)))
		case []uint64:
			r.set(uint64(len(v)))
		case map[string]uint64:
			r.set(uint64(len(v)))
		default:
			panic("count of " + class)
		}
	case "valueForKey:", "__swift_objectForKeyedSubscript:", "objectForKey:", "objectForKeyedSubscript:":
		d, ok := object.Value.(map[string]uint64)
		if !ok {
			panic("dictionary access on " + class)
		}

		r.set(d[r.collectionKey(r.arg(2))])
	case "setObject:forKey:cost:", "setValue:forKey:", "setObject:forKey:", "setObject:forKeyedSubscript:":
		d, ok := object.Value.(map[string]uint64)
		if !ok {
			panic("dictionary mutation on " + class)
		}

		key := r.collectionKey(r.arg(3))
		if r.arg(2) == 0 {
			delete(d, key)
		} else {
			d[key] = r.arg(2)
		}

		r.set(0)
	case "countByEnumeratingWithState:objects:count:":
		state, buffer, limit := r.arg(2), r.arg(3), r.arg(4)
		if limit > 100000 {
			panic("enumeration limit too large")
		}

		var items []uint64
		switch v := object.Value.(type) {
		case []uint64:
			items = v
		case map[string]uint64:
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}

			sort.Strings(keys)

			for _, k := range keys {
				items = append(items, r.New("NSString", k))
			}
		default:
			panic("invalid collection")
		}

		index := r.read64(state)
		if index >= uint64(len(items)) {
			r.set(0)

			return
		}

		count := min(limit, uint64(len(items))-index)
		for i := uint64(0); i < count; i++ {
			r.write64(buffer+i*8, items[index+i])
		}

		if object.Properties == nil {
			object.Properties = map[string]uint64{}
		}

		if object.Properties["mutationCounter"] == 0 {
			object.Properties["mutationCounter"] = r.alloc(8)
		}

		r.write64(state, index+count)
		r.write64(state+8, buffer)
		r.write64(state+16, object.Properties["mutationCounter"])
		r.set(count)
	case "enumerateKeysAndObjectsUsingBlock:":
		d := object.Value.(map[string]uint64)
		keys := make([]string, 0, len(d))

		for k := range d {
			keys = append(keys, k)
		}

		sort.Strings(keys)

		block := r.arg(2)
		stop := r.alloc(8)
		index := 0

		var next func()
		next = func() {
			if index == len(keys) || r.read64(stop) != 0 {
				r.set(0)

				return
			}

			key := keys[index]
			index++

			r.callback(block, []uint64{r.New("NSString", key), d[key], stop}, next)
		}
		next()
	case "allValues":
		d := object.Value.(map[string]uint64)
		keys := make([]string, 0, len(d))

		for k := range d {
			keys = append(keys, k)
		}

		sort.Strings(keys)

		items := make([]uint64, 0, len(d))
		for _, k := range keys {
			items = append(items, d[k])
		}

		r.set(r.New("NSArray", items))
	case "allKeys":
		d := object.Value.(map[string]uint64)
		keys := make([]string, 0, len(d))

		for k := range d {
			keys = append(keys, k)
		}

		sort.Strings(keys)

		a := make([]uint64, len(keys))
		for i, k := range keys {
			a[i] = r.New("NSString", k)
		}

		r.set(r.New("NSArray", a))
	case "doubleValue", "floatValue":
		var value float64
		switch v := object.Value.(type) {
		case float64:
			value = v
		case int64:
			value = float64(v)
		case bool:
			value = float64(boolValue(v))
		case string:
			value, _ = strconv.ParseFloat(v, 64)
		default:
			panic("unsupported numeric conversion")
		}

		bits := math.Float64bits(value)
		if selector == "floatValue" {
			bits = uint64(math.Float32bits(float32(value)))
		}

		_ = r.engine.RegWrite(40, bits)
	case "integerValue", "intValue", "longLongValue", "unsignedIntegerValue", "unsignedLongLongValue", "boolValue":
		var value int64
		switch v := object.Value.(type) {
		case float64:
			value = int64(v)
			if selector == "boolValue" {
				value = int64(boolValue(v != 0))
			}
		case int64:
			value = v
		case string:
			value, _ = strconv.ParseInt(v, 10, 64)
		case bool:
			if v {
				value = 1
			}
		}

		r.set(uint64(value))
	case "localizedDescription", "localizedFailureReason", "localizedRecoverySuggestion":
		if !r.kindOf(class, "NSError") {
			panic("unsupported localized error receiver")
		}

		fields := object.Value.(map[string]uint64)
		key := map[string]string{"localizedDescription": "NSLocalizedDescription", "localizedFailureReason": "NSLocalizedFailureReason", "localizedRecoverySuggestion": "NSLocalizedRecoverySuggestion"}[selector]

		var value uint64

		if info := r.objects[fields["userInfo"]]; info != nil {
			value = info.Value.(map[string]uint64)[key]
		}

		if value == 0 && selector == "localizedDescription" {
			value = r.New("NSString", fmt.Sprintf("%s error %d", r.String(fields["domain"]), r.objects[fields["code"]].Value))
		}

		r.set(value)
	case "domain", "userInfo", "code":
		d := object.Value.(map[string]uint64)

		value := d[selector]
		if selector == "code" {
			value = uint64(r.objects[value].Value.(int64))
		}

		r.set(value)
	case "cStringUsingEncoding:", "UTF8String":
		v := r.String(receiver)
		addr := r.alloc(uint64(len(v) + 1))
		_ = r.MemWrite(addr, append([]byte(v), 0))
		r.set(addr)
	case "dataUsingEncoding:":
		var data []byte

		switch r.arg(2) {
		case 4: // NSUTF8StringEncoding.
			data = []byte(r.String(receiver))
		case 10: // NSUnicodeStringEncoding, native ARM64 byte order with BOM.
			words := utf16.Encode([]rune(r.String(receiver)))
			data = make([]byte, 2+len(words)*2)
			binary.LittleEndian.PutUint16(data, 0xfeff)

			for index, word := range words {
				binary.LittleEndian.PutUint16(data[2+index*2:], word)
			}
		default:
			panic(fmt.Sprintf("unsupported string encoding %d", r.arg(2)))
		}

		r.set(r.New("NSData", data))
	case "removeAllObjects":
		switch object.Value.(type) {
		case []uint64:
			object.Value = []uint64{}
		case map[string]uint64:
			object.Value = map[string]uint64{}
		default:
			panic("removeAllObjects on " + class)
		}

		r.set(0)
	case "removeObjectForKey:":
		delete(object.Value.(map[string]uint64), r.collectionKey(r.arg(2)))
		r.set(0)
	case "addEntriesFromDictionary:":
		for k, v := range r.objects[r.arg(2)].Value.(map[string]uint64) {
			object.Value.(map[string]uint64)[k] = v
		}

		r.set(0)
	case "initWithArray:":
		object.Value = append([]uint64(nil), r.objects[r.arg(2)].Value.([]uint64)...)
		r.set(receiver)
	case "initWithDictionary:":
		d := map[string]uint64{}
		for k, v := range r.objects[r.arg(2)].Value.(map[string]uint64) {
			d[k] = v
		}

		object.Value = d

		r.set(receiver)
	case "initWithDictionary:copyItems:":
		r.initializeDictionary(receiver, r.arg(2), r.arg(3) != 0)
	case "rangeOfString:options:", "rangeOfString:":
		value, needle := r.String(receiver), r.String(r.arg(2))

		options := uint64(0)
		if selector == "rangeOfString:options:" {
			options = r.arg(3)
		}

		if options&1 != 0 {
			value, needle = strings.ToLower(value), strings.ToLower(needle)
		}

		index := strings.Index(value, needle)
		if options&4 != 0 {
			index = strings.LastIndex(value, needle)
		}

		start, length := uint64(0x7fffffffffffffff), uint64(0)
		if index >= 0 {
			start = uint64(len(utf16.Encode([]rune(value[:index]))))
			length = uint64(len(utf16.Encode([]rune(needle))))
		}

		r.set(start)
		_ = r.engine.RegWrite(unicorn.RegX1, length)
	case "invertedSet":
		set := r.New("NSCharacterSet", object.Value)
		r.objects[set].Properties["inverted"] = 1 - object.Properties["inverted"]
		r.set(set)
	case "stringByTrimmingCharactersInSet:":
		set := r.objects[r.arg(2)]
		r.set(r.New("NSString", strings.TrimFunc(r.String(receiver), func(c rune) bool { return characterSetContains(set, c) })))
	case "rangeOfCharacterFromSet:", "rangeOfCharacterFromSet:options:":
		set := r.objects[r.arg(2)]

		options := uint64(0)
		if selector == "rangeOfCharacterFromSet:options:" {
			options = r.arg(3)
		}

		start, length := uint64(0x7fffffffffffffff), uint64(0)
		offset := uint64(0)

		for _, c := range r.String(receiver) {
			n := uint64(len(utf16.Encode([]rune{c})))
			if characterSetContains(set, c) {
				start, length = offset, n

				if options&4 == 0 {
					break
				}
			}

			offset += n
		}

		r.set(start)
		_ = r.engine.RegWrite(unicorn.RegX1, length)
	case "substringFromIndex:", "substringToIndex:", "substringWithRange:":
		value := utf16.Encode([]rune(r.String(receiver)))

		start, end := uint64(0), uint64(len(value))

		switch selector {
		case "substringFromIndex:":
			start = r.arg(2)
		case "substringToIndex:":
			end = r.arg(2)
		case "substringWithRange:":
			start = r.arg(2)
			end = start + r.arg(3)
		}

		if end > uint64(len(value)) || start > end {
			panic("substring range exceeds string")
		}

		r.set(r.New("NSString", string(utf16.Decode(value[start:end]))))
	case "hasSuffix:":
		r.set(boolValue(strings.HasSuffix(r.String(receiver), r.String(r.arg(2)))))
	case "containsString:":
		r.set(boolValue(strings.Contains(r.String(receiver), r.String(r.arg(2)))))
	case "caseInsensitiveCompare:":
		r.set(uint64(strings.Compare(strings.ToLower(r.String(receiver)), strings.ToLower(r.String(r.arg(2))))))
	case "hasPrefix:":
		r.set(boolValue(strings.HasPrefix(r.String(receiver), r.String(r.arg(2)))))
	default:
		panic("unsupported instance message " + methodKey(class, selector, false))
	}
}

func (r *Runtime) responds(receiver uint64, selector string) bool {
	if receiver == 0 {
		return false
	}

	class, meta := r.classNames[receiver]
	object := r.objects[receiver]

	if !meta {
		if object == nil {
			return false
		}

		class = object.Class
	}

	if r.lookup(class, selector, meta) != 0 {
		return true
	}

	if meta {
		return selector == "class" || selector == "alloc" || selector == "new"
	}

	switch selector {
	case "class", "retain", "release", "autorelease", "copy", "mutableCopy", "isKindOfClass:", "isEqual:", "respondsToSelector:":
		return true
	}

	switch object.Value.(type) {
	case string:
		switch selector {
		case "length", "isEqualToString:", "hasPrefix:", "UTF8String", "integerValue", "intValue", "longLongValue", "unsignedIntegerValue", "unsignedLongLongValue", "boolValue":
			return true
		}
	case int64:
		switch selector {
		case "integerValue", "intValue", "longLongValue", "unsignedIntegerValue", "unsignedLongLongValue", "boolValue":
			return true
		}
	case map[string]uint64:
		switch selector {
		case "count", "objectForKey:", "objectForKeyedSubscript:", "allKeys", "setObject:forKey:", "setObject:forKeyedSubscript:":
			return true
		}
	case []uint64, []byte:
		return selector == "count" || selector == "length"
	}

	return false
}

func (r *Runtime) format(format string) string {
	sp, _ := r.engine.RegRead(unicorn.RegSP)

	return r.formatAt(format, sp)
}
func (r *Runtime) formatAt(format string, sp uint64) string {
	offset := uint64(0)

	var result strings.Builder

	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			result.WriteByte(format[i])

			continue
		}

		i++
		if i >= len(format) {
			panic("incomplete format")
		}

		if format[i] == '%' {
			result.WriteByte('%')

			continue
		}

		widthStart := i

		for i < len(format) && format[i] >= '0' && format[i] <= '9' {
			i++
		}

		width := format[widthStart:i]

		for i < len(format) && (format[i] == 'l' || format[i] == 'z') {
			i++
		}

		if i >= len(format) {
			panic("invalid format")
		}

		value := r.read64(sp + offset)
		offset += 8

		switch format[i] {
		case '@':
			if value == 0 {
				result.WriteString("(null)")
			} else {
				obj := r.objects[value]
				if obj == nil {
					panic(fmt.Sprintf("unknown format object %#x", value))
				}

				result.WriteString(fmt.Sprint(obj.Value))
			}
		case 'd', 'i':
			result.WriteString(strconv.FormatInt(int64(value), 10))
		case 'u':
			result.WriteString(strconv.FormatUint(value, 10))
		case 'x', 'X', 'p':
			result.WriteString(fmt.Sprintf("%"+width+string(format[i]), value))
		case 's':
			result.WriteString(r.cstring(value))
		default:
			panic("unsupported format " + format)
		}
	}

	return result.String()
}

func (r *Runtime) bundle() uint64 {
	obj := r.New("NSBundle", nil)
	r.objects[obj].Properties = map[string]uint64{"bundleIdentifier": r.New("NSString", "me.majd.ipatool"), "bundlePath": r.New("NSString", "/ipatool")}

	return obj
}
func (r *Runtime) bundleInfo() uint64 {
	return r.New("NSDictionary", map[string]uint64{"CFBundleIdentifier": r.New("NSString", "me.majd.ipatool"), "CFBundleVersion": r.New("NSString", "1.0"), "CFBundleExecutable": r.New("NSString", "ipatool"), "CFBundleName": r.New("NSString", "ipatool")})
}

func (r *Runtime) foundationCopy(receiver uint64, mutable bool) uint64 {
	obj := r.objects[receiver]
	switch obj.Class {
	case "NSArray", "NSMutableArray", "NSSet", "NSMutableSet":
		class := "NSArray"
		if strings.Contains(obj.Class, "Set") {
			class = "NSSet"
		}

		if mutable {
			class = "NSMutable" + strings.TrimPrefix(class, "NS")
		}

		return r.New(class, append([]uint64(nil), obj.Value.([]uint64)...))
	case "NSDictionary", "NSMutableDictionary":
		class := "NSDictionary"
		if mutable {
			class = "NSMutableDictionary"
		}

		d := map[string]uint64{}
		for k, v := range obj.Value.(map[string]uint64) {
			d[k] = v
		}

		return r.New(class, d)
	case "NSMutableString":
		return r.New("NSString", obj.Value)
	default:
		return receiver
	}
}

// copyItems copies each immediate value, using its native copyWithZone: when
// available. Nested collection contents remain shared, as in Foundation.
func (r *Runtime) initializeDictionary(receiver, source uint64, copyItems bool) {
	values := map[string]uint64{}
	for key, value := range r.objects[source].Value.(map[string]uint64) {
		values[key] = value
	}

	r.objects[receiver].Value = values
	if !copyItems {
		r.set(receiver)

		return
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	index := 0

	var next func()
	next = func() {
		for index < len(keys) {
			key := keys[index]
			value := values[key]
			index++

			if entry := r.lookup(r.objects[value].Class, "copyWithZone:", false); entry != 0 {
				r.callbackFunction(entry, []uint64{value, r.selector("copyWithZone:"), 0}, func() {
					values[key] = r.arg(0)

					next()
				})

				return
			}

			values[key] = r.foundationCopy(value, false)
		}

		r.set(receiver)
	}
	next()
}

func (r *Runtime) collectionKey(address uint64) string {
	if obj := r.objects[address]; obj != nil {
		switch v := obj.Value.(type) {
		case string:
			return v
		case int64:
			return fmt.Sprintf("#number:%d", v)
		}
	}

	return fmt.Sprintf("#object:%x", address)
}

func (r *Runtime) foundationEqual(a, b uint64) bool {
	if a == b {
		return true
	}

	x, y := r.objects[a], r.objects[b]
	if x == nil || y == nil {
		return false
	}

	switch v := x.Value.(type) {
	case time.Time:
		w, ok := y.Value.(time.Time)

		return ok && v.Equal(w)
	case string:
		w, ok := y.Value.(string)

		return ok && v == w
	case int64:
		switch w := y.Value.(type) {
		case int64:
			return v == w
		case bool:
			return v == int64(boolValue(w))
		}
	case bool:
		switch w := y.Value.(type) {
		case int64:
			return int64(boolValue(v)) == w
		case bool:
			return v == w
		}
	case []byte:
		w, ok := y.Value.([]byte)

		return ok && bytes.Equal(v, w)
	case []uint64:
		w, ok := y.Value.([]uint64)
		if !ok || len(v) != len(w) {
			return false
		}

		for i, p := range v {
			if !r.foundationEqual(p, w[i]) {
				return false
			}
		}

		return true
	case map[string]uint64:
		w, ok := y.Value.(map[string]uint64)
		if !ok || len(v) != len(w) {
			return false
		}

		for k, p := range v {
			q, found := w[k]
			if !found || !r.foundationEqual(p, q) {
				return false
			}
		}

		return true
	}

	return false
}

func characterSetContains(set *Object, c rune) bool {
	chars := set.Value.(string)
	match := strings.ContainsRune(chars, c)

	if chars == "0123456789" {
		match = unicode.IsDigit(c)
	}

	if set.Properties["inverted"] != 0 {
		match = !match
	}

	return match
}

func (r *Runtime) newUUIDString() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}

	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128

	return fmt.Sprintf("%X-%X-%X-%X-%X", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
