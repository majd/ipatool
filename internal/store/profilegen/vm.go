package profilegen

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"strconv"

	"github.com/majd/ipatool/v2/internal/sap/machimage"
	"github.com/majd/ipatool/v2/internal/sap/unicorn"
	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
)

const bootHeap = uint64(0x30000000000)
const bootStack = uint64(0x31000000000)
const bootReturn = uint64(0x100000000)
const bootHeapSize = uint64(64 << 20)

type bootstrapVM struct {
	metadata        *profileformat.Profile
	metadataReturns map[uint64][]metadataReturn
	atomics         map[uint64]uint32
	ctx             context.Context
	source          *source
	engine          *unicorn.Engine
	mapped          map[uint64]bool
	cursor          uint64
	mainHeader      uint64
	allocations     map[uint64]uint64
	tls             map[uint64]uint64
	callbacks       map[uint64]func()
	fault           error
	steps           uint64
	recent          []string
}

func newBootstrapVM(ctx context.Context, s *source) (*bootstrapVM, error) {
	e, err := unicorn.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create bootstrap VM: %w", err)
	}

	v := &bootstrapVM{metadata: newMetadataProfile(), ctx: ctx, source: s, engine: e, mapped: map[uint64]bool{}, atomics: map[uint64]uint32{}, allocations: map[uint64]uint64{}, tls: map[uint64]uint64{}, callbacks: map[uint64]func(){}}

	for _, r := range [][2]uint64{{bootHeap, bootHeapSize}, {bootStack, 8 << 20}, {bootReturn, 4096}} {
		if err = e.MemMap(r[0], r[1]); err != nil {
			e.Close()

			return nil, fmt.Errorf("create bootstrap VM: %w", err)
		}
	}

	if _, err = e.AddMemoryFaultHook(v.pageFault); err != nil {
		e.Close()

		return nil, fmt.Errorf("create bootstrap VM: %w", err)
	}

	if _, err = e.AddCodeHook(1, 0, v.step); err != nil {
		e.Close()

		return nil, fmt.Errorf("create bootstrap VM: %w", err)
	}

	if env := s.exports["_environ"]; env != 0 {
		v.put(env, v.alloc(8))
	}

	return v, nil
}

//nolint:nonamedreturns // Callback recovery must return false after a guest fault.
func (v *bootstrapVM) pageFault(address uint64, size uint32) (ok bool) {
	defer func() {
		if p := recover(); p != nil {
			pc, _ := v.engine.RegRead(unicorn.RegPC)
			code, _ := v.engine.MemRead(pc-16, 48)

			regs := make([]uint64, 29)
			for i := range regs {
				regs[i] = v.arg(i)
			}

			lr, _ := v.engine.RegRead(unicorn.RegLR)
			v.fault = fmt.Errorf("bootstrap memory at %#x (%s), address %#x size %d: %v; code=%x regs=%x lr=%#x; recent %v", pc, v.source.describe(pc), address, size, p, code, regs, lr, v.recent)
			ok = false
		}
	}()

	if size > 1<<20 {
		panic("oversized guest access")
	}

	for page := address &^ 4095; page < (address+uint64(size)+4095)&^4095; page += 4096 {
		if v.mapped[page] {
			continue
		}

		data := v.source.read(page, 4096)

		for off := uint64(0); off < 4096; off += 4 {
			if inside(page+off, v.source.code) {
				word := binary.LittleEndian.Uint32(data[off:])
				if isBootstrapAtomic(word) {
					v.atomics[page+off] = word
					word = 0xd503201f
				}

				binary.LittleEndian.PutUint32(data[off:], bootstrapInstruction(word))
			}
		}

		if e := v.engine.MemMap(page, 4096); e != nil {
			panic(e)
		}

		if e := v.engine.MemWrite(page, data); e != nil {
			panic(e)
		}

		v.mapped[page] = true
	}

	return true
}
func (v *bootstrapVM) read(a, n uint64) []byte {
	if n > bootHeapSize || a > ^uint64(0)-n-4095 {
		panic("read too large")
	}

	for p := a &^ 4095; p < (a+n+4095)&^4095; p += 4096 {
		if p >= bootHeap && p < bootHeap+bootHeapSize || p >= bootStack && p < bootStack+8<<20 {
			continue
		}

		if !v.mapped[p] && !v.pageFault(p, 4096) {
			panic(v.fault)
		}
	}

	b, e := v.engine.MemRead(a, n)
	if e != nil {
		panic(e)
	}

	return b
}
func (v *bootstrapVM) write(a uint64, b []byte) {
	v.read(a, uint64(len(b)))

	if e := v.engine.MemWrite(a, b); e != nil {
		panic(e)
	}
}
func (v *bootstrapVM) u64(a uint64) uint64 { return binary.LittleEndian.Uint64(v.read(a, 8)) }
func (v *bootstrapVM) put(a, x uint64) {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, x)
	v.write(a, b)
}
func (v *bootstrapVM) alloc(n uint64) uint64 {
	if n > bootHeapSize || v.cursor > bootHeapSize {
		panic("bootstrap heap exhausted")
	}

	n = (n + 15) &^ 15
	if n == 0 {
		n = 16
	}

	if n > bootHeapSize-v.cursor {
		panic("bootstrap heap exhausted")
	}

	a := bootHeap + v.cursor
	v.cursor += n
	v.allocations[a] = n

	return a
}
func (v *bootstrapVM) arg(i int) uint64 {
	x, e := v.engine.RegRead(unicorn.RegX0 + i)
	if e != nil {
		panic(e)
	}

	return x
}
func (v *bootstrapVM) set(i int, x uint64) {
	v.writeRegister(unicorn.RegX0+i, x)
}
func (v *bootstrapVM) string(a uint64) string {
	if a == 0 {
		return ""
	}

	var b []byte
	for len(b) < 1<<20 {
		p := v.read(a, 1)
		if p[0] == 0 {
			return string(b)
		}

		b = append(b, p[0])
		a++
	}

	panic("bootstrap string too long")
}
func (v *bootstrapVM) cstring(s string) uint64 {
	a := v.alloc(uint64(len(s) + 1))
	v.write(a, append([]byte(s), 0))

	return a
}
func (v *bootstrapVM) call(name string, args ...uint64) (uint64, uint64, error) {
	entry := v.source.exports["_"+name]
	if entry == 0 {
		return 0, 0, fmt.Errorf("bootstrap export %s is missing", name)
	}

	return v.callAddress(entry, args...)
}
func (v *bootstrapVM) callAddress(entry uint64, args ...uint64) (uint64, uint64, error) {
	name := v.source.describe(entry)

	for i := 0; i < 8; i++ {
		x := uint64(0)
		if i < len(args) {
			x = args[i]
		}

		v.set(i, x)
	}

	v.writeRegister(unicorn.RegSP, bootStack+(8<<20)-4096)
	v.writeRegister(unicorn.RegLR, bootReturn)
	err := v.engine.StartBounded(entry, bootReturn, 30*time.Second, 20_000_000)

	if v.fault != nil {
		return 0, 0, v.fault
	}

	pc, _ := v.engine.RegRead(unicorn.RegPC)
	if err != nil || pc != bootReturn {
		return 0, 0, fmt.Errorf("bootstrap %s stopped at %#x: %v; recent %v", name, pc, fmt.Errorf("%v bytes=%x loc=%s", err, v.read(pc, 16), v.source.describe(pc)), v.recent)
	}

	v.observeMetadataReturn(bootReturn)

	return v.arg(0), v.arg(1), nil
}
func (v *bootstrapVM) step(pc uint64, _ uint32) {
	defer func() {
		if p := recover(); p != nil {
			v.fault = fmt.Errorf("bootstrap at %#x: %v; recent %v", pc, p, v.recent)
			_ = v.engine.Stop()
		}
	}()

	v.observeMetadataReturn(pc)

	if word, ok := v.atomics[pc]; ok {
		v.atomic(word)
		v.writeRegister(unicorn.RegPC, pc+4)

		return
	}

	if f := v.callbacks[pc]; f != nil {
		f()

		return
	}
	// Shared-cache branch islands are outside individual Mach-O sections.
	if !inside(pc, v.source.code) {
		raw, e := v.engine.MemRead(pc, 4)
		if e != nil {
			panic(e)
		}

		word := binary.LittleEndian.Uint32(raw)
		replacement := machimage.WithoutPointerAuthentication(word)

		if replacement != word {
			switch {
			case replacement == 0xd503201f:
				v.writeRegister(unicorn.RegPC, pc+4)
			case replacement == 0xd65f03c0:
				lr, _ := v.engine.RegRead(unicorn.RegLR)
				v.writeRegister(unicorn.RegPC, lr)
			case replacement&0xfffffc1f == 0xd61f0000:
				v.writeRegister(unicorn.RegPC, v.arg(int((replacement>>5)&31)))
			case replacement&0xfffffc1f == 0xd63f0000:
				target := v.arg(int((replacement >> 5) & 31))
				v.writeRegister(unicorn.RegLR, pc+4)
				v.writeRegister(unicorn.RegPC, target)
			default:
				panic("unsupported PAC replacement")
			}

			return
		}
	}

	v.steps++
	if v.steps%4096 == 0 {
		if err := v.ctx.Err(); err != nil {
			panic(err)
		}
	}

	name := v.source.symbols[pc]
	if name == "" {
		return
	}

	if len(v.recent) == 0 || v.recent[len(v.recent)-1] != name {
		v.recent = append(v.recent, name)
		if len(v.recent) > 16 {
			v.recent = v.recent[1:]
		}
	}

	v.observeMetadataCall(name)

	if v.host(name) {
		lr, _ := v.engine.RegRead(unicorn.RegLR)
		v.writeRegister(unicorn.RegPC, lr)
	}
}
func (v *bootstrapVM) host(name string) bool {
	switch name {
	case "os_log_create":
		v.set(0, v.alloc(16))
	case "os_log_type_enabled", "os_signpost_enabled":
		v.set(0, 0)
	case "_os_log_impl", "_os_log_error_impl", "_os_log_fault_impl":
		return true

	case "csops":
		if v.arg(1) != 0 || v.arg(3) != 4 {
			panic("unsupported bootstrap code-signing query")
		}

		v.write(v.arg(2), make([]byte, 4))
		v.set(0, 0)

	case "getpid":
		v.set(0, 1)
	case "getuid", "geteuid", "getgid", "getegid", "getppid":
		v.set(0, 0)

	case "dyld_program_sdk_at_least", "dyld_program_minos_at_least":
		requested := v.arg(0)
		platform, version := uint32(requested), uint32(requested>>32)
		ok := platform == 1 && version <= 0x1b0001 || platform == ^uint32(0) && version>>16 <= 2026

		v.set(0, 0)

		if ok {
			v.set(0, 1)
		}

	case "objc_readClassPair", "_objc_realizeClassFromSwift":
		return true
	case "objc_registerClassPair":
		return true

	case "objc_msgSend":
		receiver, selector := v.arg(0), v.string(v.arg(1))
		if receiver == 0 {
			v.set(0, 0)

			return true
		}

		switch selector {
		case "class":
			if _, ok := v.source.classes[receiver]; ok {
				v.set(0, receiver)
			} else {
				v.set(0, v.u64(receiver))
			}
		default:
			panic(fmt.Sprintf("unhandled bootstrap ObjC message %s to %#x", selector, receiver))
		}
	case "objc_opt_self", "objc_retain", "objc_autorelease":
		return true
	case "objc_release":
		return true
	case "objc_opt_class":
		receiver := v.arg(0)
		if receiver != 0 {
			if _, ok := v.source.classes[receiver]; ok {
				v.set(0, receiver)
			} else {
				v.set(0, v.u64(receiver))
			}
		}

	case "_os_feature_enabled_simple_impl":
		v.set(0, 0)

	case "dlopen":
		name := v.string(v.arg(0))
		if f := v.source.images[name]; f != nil {
			v.set(0, f.GetBaseAddress())
		} else {
			v.set(0, 0)
		}
	case "dlsym":
		v.set(0, v.source.exports["_"+v.string(v.arg(1))])
	case "dlerror":
		v.set(0, 0)

	case "objc_getClass", "objc_lookUpClass":
		name := className(v.string(v.arg(0)))
		v.set(0, 0)

		for _, c := range v.source.classes {
			if c.Name == name {
				v.set(0, c.Address)

				break
			}
		}
	case "class_getName":
		class := v.arg(0)
		if c := v.source.classes[class]; c != nil {
			v.set(0, v.cstring(c.Name))
		} else {
			v.set(0, v.cstring("nil"))
		}
	case "class_getSuperclass":
		v.set(0, v.u64(v.arg(0)+8))
	case "class_getInstanceSize":
		if c := v.source.classes[v.arg(0)]; c != nil {
			v.set(0, c.Size)
		} else {
			panic("unknown ObjC class size")
		}
	case "object_getClass":
		if v.arg(0) != 0 {
			v.set(0, v.u64(v.arg(0)))
		}

	case "thread_chkstk_darwin", "___chkstk_darwin", "__chkstk_darwin":
		return true

	case "__cxa_guard_acquire":
		guard := v.u64(v.arg(0))
		if guard&255 != 0 {
			v.set(0, 0)
		} else if guard&0xff00 != 0 {
			panic("recursive C++ static initialization")
		} else {
			v.put(v.arg(0), 0x100)
			v.set(0, 1)
		}
	case "__cxa_guard_release":
		v.put(v.arg(0), 1)
	case "__cxa_guard_abort":
		v.put(v.arg(0), 0)
	case "pthread_mach_thread_np":
		v.set(0, 1)
	case "pthread_key_create":
		key := uint64(len(v.tls) + 1)
		v.tls[key] = 0
		v.put(v.arg(0), key)
		v.set(0, 0)
	case "pthread_key_delete":
		delete(v.tls, v.arg(0))
		v.set(0, 0)
	case "os_unfair_lock_lock_with_options":
		v.set(0, 0)

	case "malloc_type_zone_malloc_with_options", "malloc_zone_malloc_with_options", "malloc_zone_memalign", "malloc_type_zone_memalign", "posix_memalign", "malloc_type_posix_memalign":
		alignment, n := v.arg(1), v.arg(2)
		if alignment == 0 || alignment&(alignment-1) != 0 || alignment > bootHeapSize {
			panic("invalid bootstrap alignment")
		}

		if v.cursor > bootHeapSize {
			panic("bootstrap heap exhausted")
		}

		v.cursor = (v.cursor + alignment - 1) &^ (alignment - 1)
		address := v.alloc(n)

		if strings.Contains(name, "posix") {
			v.put(v.arg(0), address)
			v.set(0, 0)
		} else {
			v.set(0, address)
		}
	case "malloc_size":
		v.set(0, v.allocations[v.arg(0)])
	case "malloc_good_size":
		v.set(0, (v.arg(0)+15)&^15)
	case "realloc", "malloc_type_realloc", "malloc_zone_realloc", "malloc_type_zone_realloc":
		p, n := v.arg(0), v.arg(1)
		if strings.Contains(name, "_zone_") {
			p, n = v.arg(1), v.arg(2)
		}

		if n == 0 {
			v.set(0, 0)

			return true
		}

		a := v.alloc(n)
		if p != 0 {
			v.write(a, v.read(p, min(v.allocations[p], n)))
		}

		v.set(0, a)

	case "_dyld_get_shared_cache_range":
		base, size := v.source.cache.AddressRange()
		if output := v.arg(0); output != 0 {
			v.put(output, size)
		}

		v.set(0, base)
	case "_dyld_is_memory_immutable", "_dyld_has_preoptimized_swift_protocol_conformances", "_dyld_get_swift_prespecialized_data", "dyld_shared_cache_some_image_overridden", "_dyld_swift_optimizations_version":
		v.set(0, 0)

	case "objc_addLoadImageFunc2", "objc_addLoadImageFunc", "_dyld_register_func_for_add_image":
		fn := v.arg(0)
		names := v.source.imageNames()
		index := 0

		var next func()
		next = func() {
			if index == len(names) {
				v.set(0, 0)

				return
			}

			f := v.source.images[names[index]]
			index++

			v.invoke(fn, []uint64{f.GetBaseAddress(), 0}, next)
		}
		next()

		return false
	case "_dyld_lookup_section_info":
		header, kind := v.arg(0), v.arg(2)
		v.set(0, 0)
		v.set(1, 0)

		sections := []string{"__swift5_protos", "__swift5_proto", "__swift5_types", "__swift5_replace", "__swift5_replac2", "__swift5_acfuncs", "__swift5_ac_pro"}
		if kind >= uint64(len(sections)) {
			panic("unknown dyld section kind")
		}

		for _, f := range v.source.images {
			if f.GetBaseAddress() == header {
				for _, sec := range f.Sections {
					if sec.Name == sections[kind] {
						v.set(0, sec.Addr)
						v.set(1, sec.Size)

						return true
					}
				}

				break
			}
		}

	case "_NSGetMachExecuteHeader":
		if v.mainHeader == 0 {
			v.mainHeader = v.alloc(32)
			header := make([]byte, 32)
			binary.LittleEndian.PutUint32(header, 0xfeedfacf)
			binary.LittleEndian.PutUint32(header[4:], 0x100000c)
			binary.LittleEndian.PutUint32(header[12:], 2)
			v.write(v.mainHeader, header)
		}

		v.set(0, v.mainHeader)

	case "swift_once", "dispatch_once_f":
		token, fn, arg := v.arg(0), v.arg(1), v.arg(2)
		if name == "dispatch_once_f" {
			fn, arg = arg, fn
		}

		if v.u64(token) == ^uint64(0) {
			return true
		}

		if v.u64(token) != 0 {
			panic("recursive bootstrap once")
		}

		v.put(token, 1)
		v.invoke(fn, []uint64{arg}, func() { v.put(token, ^uint64(0)) })

		return false
	case "_ZnwmSt19__type_descriptor_t", "_ZnamSt19__type_descriptor_t", "_Znwm", "_Znam", "malloc", "calloc", "malloc_zone_malloc", "malloc_zone_calloc", "malloc_type_malloc", "malloc_type_calloc", "malloc_type_zone_malloc", "malloc_type_zone_calloc":
		n := v.arg(0)
		if strings.Contains(name, "_zone_") {
			n = v.arg(1)
		}

		if strings.HasSuffix(name, "calloc") {
			if !strings.Contains(name, "_zone_") {
				if v.arg(1) != 0 && n > bootHeapSize/v.arg(1) {
					panic("bootstrap calloc overflow")
				}

				n *= v.arg(1)
			} else {
				if v.arg(2) != 0 && n > bootHeapSize/v.arg(2) {
					panic("bootstrap calloc overflow")
				}

				n *= v.arg(2)
			}
		}

		v.set(0, v.alloc(n))
	case "free", "malloc_zone_free", "pthread_mutex_lock", "pthread_mutex_unlock", "pthread_mutex_init", "pthread_rwlock_rdlock", "pthread_rwlock_wrlock", "pthread_rwlock_unlock", "os_unfair_lock_lock", "os_unfair_lock_unlock":
		v.set(0, 0)
	case "memcpy", "memmove":
		a := v.arg(0)
		v.write(a, v.read(v.arg(1), v.arg(2)))
		v.set(0, a)
	case "memset":
		a, n := v.arg(0), v.arg(2)
		if n > bootHeapSize {
			panic("bootstrap memset too large")
		}

		v.write(a, bytes.Repeat([]byte{byte(v.arg(1))}, int(n)))
		v.set(0, a)
	case "bzero":
		if v.arg(1) > bootHeapSize {
			panic("bootstrap bzero too large")
		}

		v.write(v.arg(0), make([]byte, v.arg(1)))
	case "memcmp":
		v.set(0, uint64(int64(bytes.Compare(v.read(v.arg(0), v.arg(2)), v.read(v.arg(1), v.arg(2))))))
	case "strlen":
		v.set(0, uint64(len(v.string(v.arg(0)))))
	case "strcmp":
		v.set(0, uint64(int64(strings.Compare(v.string(v.arg(0)), v.string(v.arg(1))))))
	case "getenv":
		v.set(0, 0)
	case "getpagesize":
		v.set(0, 16384)
	case "pthread_self":
		v.set(0, bootStack)
	case "pthread_getspecific":
		v.set(0, v.tls[v.arg(0)])
	case "pthread_setspecific":
		v.tls[v.arg(0)] = v.arg(1)
		v.set(0, 0)
	case "abort", "__assert_rtn", "__stack_chk_fail":
		panic("guest called " + name)
	default:
		return false
	}

	return true
}

func (v *bootstrapVM) invoke(fn uint64, args []uint64, after func()) {
	link, _ := v.engine.RegRead(unicorn.RegLR)
	resume := v.alloc(16)
	v.write(resume, []byte{0xc0, 3, 0x5f, 0xd6})

	v.callbacks[resume] = func() {
		delete(v.callbacks, resume)
		v.writeRegister(unicorn.RegLR, link)
		v.writeRegister(unicorn.RegPC, link)
		after()
	}
	for i, a := range args {
		v.set(i, a)
	}

	v.writeRegister(unicorn.RegLR, resume)
	v.writeRegister(unicorn.RegPC, fn)
}

// Bootstrap is single-threaded: RCpc acquire loads have ordinary load semantics.
// Unicorn 2.1 does not decode LDAPR even with its maximum ARM64 CPU model.
func bootstrapInstruction(word uint32) uint32 {
	switch word & 0xfffffc00 {
	case 0xf8bfc000:
		return 0xf9400000 | word&0x3ff
	case 0xb8bfc000:
		return 0xb9400000 | word&0x3ff
	case 0x78bfc000:
		return 0x79400000 | word&0x3ff
	case 0x38bfc000:
		return 0x39400000 | word&0x3ff
	}

	return machimage.WithoutPointerAuthentication(word)
}

func (v *bootstrapVM) reg(n uint32) uint64 {
	switch n {
	case 31:
		return 0
	case 30:
		x, _ := v.engine.RegRead(unicorn.RegLR)

		return x
	case 29:
		x, _ := v.engine.RegRead(unicorn.RegX29)

		return x
	default:
		return v.arg(int(n))
	}
}
func (v *bootstrapVM) setReg(n uint32, x uint64) {
	switch n {
	case 31:
		return
	case 30:
		v.writeRegister(unicorn.RegLR, x)
	case 29:
		v.writeRegister(unicorn.RegX29, x)
	default:
		v.set(int(n), x)
	}
}
func (v *bootstrapVM) atomic(word uint32) {
	if word&0x3fa07c00 == 0x08a07c00 || word&0xbfa07c00 == 0x08207c00 {
		v.compareExchange(word)

		return
	}

	n, t, s := (word>>5)&31, word&31, (word>>16)&31
	address := v.reg(n)

	if n == 31 {
		address, _ = v.engine.RegRead(unicorn.RegSP)
	}

	size := uint64(1) << ((word >> 30) & 3)

	var b [8]byte

	copy(b[:], v.read(address, size))
	old := binary.LittleEndian.Uint64(b[:])
	rhs := v.reg(s)
	mask := ^uint64(0) >> (64 - size*8)
	rhs &= mask
	value := old
	signed := func(x uint64) int64 { return int64(x<<(64-size*8)) >> (64 - size*8) }

	switch (word >> 12) & 15 {
	case 0:
		value = old + rhs
	case 1:
		value = old &^ rhs
	case 2:
		value = old ^ rhs
	case 3:
		value = old | rhs
	case 4:
		if signed(rhs) > signed(old) {
			value = rhs
		}
	case 5:
		if signed(rhs) < signed(old) {
			value = rhs
		}
	case 6:
		value = max(old, rhs)
	case 7:
		value = min(old, rhs)
	case 8:
		value = rhs
	default:
		panic("unsupported atomic")
	}

	binary.LittleEndian.PutUint64(b[:], value&mask)
	v.write(address, b[:size])
	v.setReg(t, old)
}

func isBootstrapAtomic(word uint32) bool {
	return word&0x3f200c00 == 0x38200000 && (word>>12)&15 <= 8 || word&0x3fa07c00 == 0x08a07c00 || word&0xbfa07c00 == 0x08207c00
}
func (v *bootstrapVM) compareExchange(word uint32) {
	n, t, s := (word>>5)&31, word&31, (word>>16)&31
	address := v.reg(n)

	if n == 31 {
		address, _ = v.engine.RegRead(unicorn.RegSP)
	}

	size := uint64(1) << ((word >> 30) & 3)
	count := uint32(1)

	if word&0xbfa07c00 == 0x08207c00 {
		size = 4 << ((word >> 30) & 1)
		count = 2
	}

	mask := ^uint64(0) >> (64 - size*8)
	old := make([]uint64, count)
	match := true

	for i := uint32(0); i < count; i++ {
		var b [8]byte

		copy(b[:], v.read(address+uint64(i)*size, size))
		old[i] = binary.LittleEndian.Uint64(b[:])
		match = match && old[i] == v.reg(s+i)&mask
	}

	if match {
		for i := uint32(0); i < count; i++ {
			var b [8]byte

			binary.LittleEndian.PutUint64(b[:], v.reg(t+i)&mask)
			v.write(address+uint64(i)*size, b[:size])
		}
	}

	for i := uint32(0); i < count; i++ {
		v.setReg(s+i, old[i])
	}
}

func (v *bootstrapVM) writeRegister(register int, value uint64) {
	if err := v.engine.RegWrite(register, value); err != nil {
		panic(err)
	}
}

type metadataReturn struct {
	Stack            uint64
	Name, Key, Alias string
}

// observeMetadataCall records the caller's return address and stack pointer.
// Matching both associates results with the correct nested guest invocation.
func (v *bootstrapVM) observeMetadataCall(name string) {
	if v.metadata == nil {
		return
	}

	var key, alias string

	switch name {
	case "swift_getSingletonMetadata", "swift_getForeignTypeMetadata":
		key = strconv.FormatUint(v.arg(1), 10)
	case "swift_getWitnessTable":
		key = profileformat.WitnessKey(v.arg(0), v.arg(1))
		alias = strconv.FormatUint(v.arg(0), 10)
	default:
		return
	}

	link, _ := v.engine.RegRead(unicorn.RegLR)
	stack, _ := v.engine.RegRead(unicorn.RegSP)

	if v.metadataReturns == nil {
		v.metadataReturns = map[uint64][]metadataReturn{}
	}

	v.metadataReturns[link] = append(v.metadataReturns[link], metadataReturn{stack, name, key, alias})
}

func (v *bootstrapVM) observeMetadataReturn(pc uint64) {
	pending := v.metadataReturns[pc]
	if len(pending) == 0 {
		return
	}

	stack, _ := v.engine.RegRead(unicorn.RegSP)
	remaining := pending[:0]

	for _, r := range pending {
		if r.Stack != stack {
			remaining = append(remaining, r)

			continue
		}

		var target map[string]uint64

		switch r.Name {
		case "swift_getSingletonMetadata":
			target = v.metadata.SwiftSingletons
		case "swift_getForeignTypeMetadata":
			target = v.metadata.SwiftForeign
		case "swift_getWitnessTable":
			target = v.metadata.SwiftWitnesses
		}

		if value := v.arg(0); value != 0 && (r.Name == "swift_getWitnessTable" || v.arg(1) == 0) {
			target[r.Key] = value
			// Native reference profiles use descriptor-only keys. Execution
			// uses the metadata-specific key to distinguish generic instances.
			if r.Alias != "" && target[r.Alias] == 0 {
				target[r.Alias] = value
			}
		}
	}

	if len(remaining) == 0 {
		delete(v.metadataReturns, pc)
	} else {
		v.metadataReturns[pc] = remaining
	}
}
