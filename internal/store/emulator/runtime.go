// Package emulator is the Objective-C runtime bridge for the pinned Store image.
package emulator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/blacktop/go-macho"
	"github.com/majd/ipatool/v2/internal/sap/machimage"
	"github.com/majd/ipatool/v2/internal/sap/unicorn"
	storeprofile "github.com/majd/ipatool/v2/internal/store/profile"
	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
)

const heapBase = uint64(0x400000000000)
const heapSize = uint64(64 << 20)
const stackBase = uint64(0x500000000000)
const stackSize = uint64(8 << 20)
const returnPC = uint64(0x100000000)

type Method = profileformat.Method
type Class = profileformat.Class
type Call = profileformat.Call
type Page = profileformat.Page
type Constant = profileformat.Constant
type DictionaryLayout = profileformat.DictionaryLayout
type MetadataPatch = profileformat.MetadataPatch
type Profile = profileformat.Profile

type Object struct {
	Class      string
	Value      any
	Properties map[string]uint64
}
type timer struct{ at, block uint64 }
type taskLocalBinding struct{ key, value uint64 }
type Runtime struct {
	lastResponse                             *http.Response
	transportError                           error
	streamResponse, noRedirects, authRequest bool

	taskLocalFrames             map[uint64][]taskLocalBinding
	transport                   func(*http.Request) (*http.Response, error)
	inflaters                   map[uint64]*inflateStream
	responseHeaders             map[uint64]http.Header
	tasks                       map[uint64]*asyncTask
	currentTask                 *asyncTask
	nullObject                  uint64
	associations                map[uint64]map[uint64]uint64
	currentQueue, selectedQueue uint64
	preferences                 map[string]uint64
	notificationCenter          uint64
	observers                   map[uint64]*Object
	pendingQueues               map[uint64]uint64
	allocations                 map[uint64]uint64
	ctx                         context.Context
	codeRanges                  [][2]uint64
	steps                       uint64
	timers                      []timer
	callbacks                   map[uint64]func()
	pending                     []uint64
	hardwareID                  []byte
	engine                      *unicorn.Engine
	profile                     Profile
	classes                     map[string]*Class
	classNames                  map[uint64]string
	methods                     map[string]uint64
	calls                       map[uint64]Call
	selectors                   map[uint64]string
	selectorAddresses           map[string]uint64
	objects                     map[uint64]*Object
	pages                       map[uint64]bool
	cursor                      uint64
	fault                       error
	trace                       []string

	entries map[uint64]string
	hook    *unicorn.Hook
}

func Open(ctx context.Context, imageData []byte, prepared storeprofile.Data) (*Runtime, error) {
	return open(ctx, imageData, prepared, false)
}

// open optionally guards accesses against the exported page set. Validation and
// production use the same grouped mappings to stay within Unicorn section limits.
func open(ctx context.Context, imageData []byte, prepared storeprofile.Data, strict bool) (*Runtime, error) {
	if ctx == nil {
		return nil, errors.New("emulator context is nil")
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("prepare store runtime: %w", err)
	}

	if fmt.Sprintf("%x", sha256.Sum256(imageData)) != "95f4558aa2c0ccd9f96ed479e336b630b46bf4b4e0baabc1c1ac8c7d55e21ab4" {
		return nil, errors.New("AppleMediaServices does not match the pinned 26A434 image")
	}

	metadata, err := cachedMetadata.load(ctx, prepared.Metadata)
	if err != nil {
		return nil, fmt.Errorf("prepare store runtime: %w", err)
	}

	profile := metadata.profile

	imageInputs := map[string][]byte{"AppleMediaServices": imageData}

	if profile.PluginUUID != "" {
		if profile.PluginUUID != "B8C07F94-4868-3CD7-A59A-F01E8C24C1B7" {
			return nil, errors.New("unsupported authentication plugin UUID")
		}

		plugin := prepared.Plugin
		if fmt.Sprintf("%x", sha256.Sum256(plugin)) != "e33fff05a6e6593dd91e33fd103b5d3068b62a787c680df33b859f19b3acc4ea" {
			return nil, errors.New("authentication plugin does not match the pinned image")
		}

		imageInputs["AMSAccountAuthenticationPlugin"] = plugin
	}

	engine, err := unicorn.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("prepare store runtime: %w", err)
	}

	r := metadata.newRuntime()
	r.engine = engine
	r.ctx = ctx
	r.callbacks = map[uint64]func(){}
	ready := false

	defer func() {
		if !ready {
			_ = r.Close()
		}
	}()

	for _, region := range [][2]uint64{{heapBase, heapSize}, {stackBase, stackSize}, {returnPC, 4096}} {
		if err = r.MemMap(region[0], region[1]); err != nil {
			return nil, fmt.Errorf("prepare store runtime: %w", err)
		}
	}

	for name, data := range imageInputs {
		image, e := machimage.Open(name, data)
		if e != nil {
			return nil, fmt.Errorf("prepare store runtime: %w", e)
		}

		if e = image.PreparePrelinked(nil, func(symbol string) (uint64, error) { return 0, fmt.Errorf("unexpected relocation %s", symbol) }); e != nil {
			return nil, fmt.Errorf("prepare store runtime: %w", e)
		}

		if e = image.Load(r); e != nil {
			return nil, fmt.Errorf("prepare store runtime: %w", e)
		}
	}

	pageData := prepared.Pages
	addresses := make([]uint64, 0, len(profile.Pages))

	for _, page := range profile.Pages {
		if !r.pages[page.Address] {
			addresses = append(addresses, page.Address)
		}
	}

	for _, call := range profile.Calls {
		page := call.Address &^ 4095
		if !r.pages[page] {
			addresses = append(addresses, page)
		}
	}

	if strict {
		allowed := maps.Clone(r.pages)
		for _, address := range addresses {
			allowed[address] = true
		}

		if err = engine.GuardMemory(func(address, size uint64) error {
			return checkProfileAccess(allowed, address, size)
		}); err != nil {
			return nil, fmt.Errorf("guard Store memory: %w", err)
		}
	}

	regions, err := groupPages(addresses)
	if err != nil {
		return nil, fmt.Errorf("prepare store runtime: %w", err)
	}

	for _, region := range regions {
		if err = ctx.Err(); err != nil {
			return nil, fmt.Errorf("prepare store runtime: %w", err)
		}

		if err = r.MemMap(region.address, region.size); err != nil {
			return nil, fmt.Errorf("prepare store runtime: %w", err)
		}
	}

	for _, page := range profile.Pages {
		if page.Offset+4096 > uint64(len(pageData)) {
			return nil, errors.New("profile page exceeds data")
		}

		if err = r.MemWrite(page.Address, pageData[page.Offset:page.Offset+4096]); err != nil {
			return nil, fmt.Errorf("prepare store runtime: %w", err)
		}
	}

	for _, call := range profile.Calls {
		page := call.Address &^ 4095
		if !r.pages[page] {
			if err = r.MemMap(page, 4096); err != nil {
				return nil, fmt.Errorf("prepare store runtime: %w", err)
			}
		}

		if err = r.MemWrite(call.Address, []byte{0xc0, 0x03, 0x5f, 0xd6}); err != nil {
			return nil, fmt.Errorf("prepare store runtime: %w", err)
		}
	}
	// Non-fragile ivar offsets are fixed by the Objective-C loader on macOS.
	for _, imageBytes := range imageInputs {
		file, err := macho.NewFile(bytes.NewReader(imageBytes))
		if err != nil {
			return nil, fmt.Errorf("prepare store runtime: %w", err)
		}

		defer file.Close()

		for _, section := range file.Sections {
			if section.Flags.IsPureInstructions() || section.Flags.IsSomeInstructions() {
				r.codeRanges = append(r.codeRanges, [2]uint64{section.Addr, section.Addr + section.Size})
			}
		}

		if file.Symtab != nil {
			for _, sym := range file.Symtab.Syms {
				if !strings.HasPrefix(sym.Name, "_OBJC_IVAR_$_") {
					continue
				}

				parts := strings.SplitN(strings.TrimPrefix(sym.Name, "_OBJC_IVAR_$_"), ".", 2)
				if len(parts) != 2 {
					continue
				}

				if c := r.classes[parts[0]]; c != nil {
					if offset, ok := c.Ivars[parts[1]]; ok {
						data := make([]byte, 4)
						binary.LittleEndian.PutUint32(data, uint32(offset))

						if err = r.MemWrite(sym.Value, data); err != nil {
							return nil, fmt.Errorf("prepare store runtime: %w", err)
						}
					}
				}
			}
		}
	}

	for _, patch := range profile.MetadataPatches {
		if e := r.MemWrite(patch.Address, patch.Data); e != nil {
			return nil, fmt.Errorf("prepare store runtime: %w", e)
		}
	}

	r.hook, err = engine.AddCodeHook(0, ^uint64(0), r.step)
	if err != nil {
		return nil, fmt.Errorf("prepare store runtime: %w", err)
	}

	ready = true

	return r, nil
}
func methodKey(class, selector string, meta bool) string {
	prefix := "-"
	if meta {
		prefix = "+"
	}

	return prefix + "[" + class + " " + selector + "]"
}
func (r *Runtime) Close() error {
	if r.engine == nil {
		return nil
	}
	// Guest objects live for one request. Release their host references on close.
	clear(r.objects)
	clear(r.callbacks)
	clear(r.responseHeaders)
	clear(r.inflaters)
	clear(r.preferences)
	clear(r.associations)
	clear(r.observers)
	clear(r.tasks)
	r.pending = nil
	r.timers = nil
	err := r.engine.Close()
	r.engine = nil

	if err != nil {
		return fmt.Errorf("close store runtime: %w", err)
	}

	return nil
}
func (r *Runtime) MemMap(address, size uint64) error {
	overlap := false
	for page := address; page < address+size; page += 4096 {
		overlap = overlap || r.pages[page]
	}

	if !overlap {
		if err := r.engine.MemMap(address, size); err != nil {
			return fmt.Errorf("map guest memory: %w", err)
		}

		for page := address; page < address+size; page += 4096 {
			r.pages[page] = true
		}

		return nil
	}

	for page := address; page < address+size; {
		if r.pages[page] {
			page += 4096

			continue
		}

		end := page + 4096
		for end < address+size && !r.pages[end] {
			end += 4096
		}

		if err := r.engine.MemMap(page, end-page); err != nil {
			return fmt.Errorf("map guest memory: %w", err)
		}

		for ; page < end; page += 4096 {
			r.pages[page] = true
		}
	}

	return nil
}

func (r *Runtime) MemWrite(address uint64, data []byte) error {
	if err := r.engine.MemWrite(address, data); err != nil {
		return fmt.Errorf("write guest memory: %w", err)
	}

	return nil
}
func (r *Runtime) alloc(size uint64) uint64 {
	if size > heapSize {
		panic("guest allocation exceeds heap budget")
	}

	size = (size + 15) &^ 15
	if size == 0 {
		size = 16
	}

	if size > heapSize-r.cursor {
		panic("guest heap exhausted")
	}

	address := heapBase + r.cursor

	r.cursor += size
	if r.allocations == nil {
		r.allocations = map[uint64]uint64{}
	}

	r.allocations[address] = size

	return address
}
func (r *Runtime) read64(address uint64) uint64 {
	b, e := r.engine.MemRead(address, 8)
	if e != nil {
		panic(fmt.Sprintf("read guest pointer %#x: %v", address, e))
	}

	return binary.LittleEndian.Uint64(b)
}
func (r *Runtime) write64(address, value uint64) {
	var b [8]byte

	binary.LittleEndian.PutUint64(b[:], value)

	if e := r.MemWrite(address, b[:]); e != nil {
		panic(e)
	}
}
func (r *Runtime) arg(i int) uint64 {
	reg := unicorn.RegX0 + i

	if i >= 8 {
		sp, _ := r.engine.RegRead(unicorn.RegSP)

		return r.read64(sp + uint64(i-8)*8)
	}

	v, e := r.engine.RegRead(reg)
	if e != nil {
		panic(e)
	}

	return v
}
func (r *Runtime) set(value uint64) {
	if e := r.engine.RegWrite(unicorn.RegX0, value); e != nil {
		panic(e)
	}
}
func (r *Runtime) cstring(address uint64) string {
	if address == 0 {
		return ""
	}

	if value, ok := r.selectors[address]; ok {
		return value
	}

	var b []byte

	for i := uint64(0); i < 16384; i++ {
		v, e := r.engine.MemRead(address+i, 1)
		if e != nil {
			panic(fmt.Sprintf("read C string %#x: %v", address, e))
		}

		if v[0] == 0 {
			return string(b)
		}

		b = append(b, v[0])
	}

	panic("unterminated guest string")
}
func (r *Runtime) selector(value string) uint64 {
	if addr := r.selectorAddresses[value]; addr != 0 {
		return addr
	}

	addr := r.alloc(uint64(len(value) + 1))
	if err := r.MemWrite(addr, append([]byte(value), 0)); err != nil {
		panic(err)
	}

	r.selectorAddresses[value] = addr
	r.selectors[addr] = value

	return addr
}
func (r *Runtime) New(class string, value any) uint64 {
	size := uint64(256)
	if c := r.classes[class]; c != nil {
		size = max(c.Size, 16)
	}

	address := r.alloc(size)
	obj := &Object{Class: class, Value: value, Properties: map[string]uint64{}}

	r.objects[address] = obj
	if c := r.classes[class]; c != nil {
		r.write64(address, c.Address)
	}

	return address
}
func (r *Runtime) SetIvar(object uint64, name string, value uint64) error {
	obj := r.objects[object]
	if obj == nil {
		return errors.New("unknown guest object")
	}

	for class := obj.Class; class != ""; {
		c := r.classes[class]
		if c == nil {
			break
		}

		if offset, ok := c.Ivars[name]; ok {
			r.write64(object+offset, value)

			return nil
		}

		class = c.Super
	}

	return fmt.Errorf("unknown ivar %s.%s", obj.Class, name)
}
func (r *Runtime) String(address uint64) string {
	if address == 0 {
		return ""
	}

	obj := r.objects[address]
	if obj == nil {
		panic(fmt.Sprintf("unknown string object %#x", address))
	}

	value, ok := obj.Value.(string)
	if !ok {
		panic(fmt.Sprintf("object %s is not a string", obj.Class))
	}

	return value
}
func (r *Runtime) lookup(class, selector string, meta bool) uint64 {
	for class != "" {
		if address := r.methods[methodKey(class, selector, meta)]; address != 0 {
			return address
		}

		c := r.classes[class]
		if c == nil {
			break
		}

		class = c.Super
	}

	return 0
}

//nolint:nonamedreturns // Panic recovery must return an error from the ABI boundary.
func (r *Runtime) Invoke(object uint64, selector string, args ...uint64) (result uint64, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = recoveredError("guest setup", p)
		}
	}()

	class, meta := r.classNames[object]
	if !meta {
		obj := r.objects[object]
		if obj == nil {
			return 0, fmt.Errorf("unknown receiver %#x for %s", object, selector)
		}

		class = obj.Class
	}

	entry := r.lookup(class, selector, meta)
	if entry == 0 {
		block := r.hostBlock(func() { r.message(object, selector, false) })
		entry = r.read64(block + 16)
	}

	arguments := append([]uint64{object, r.selector(selector)}, args...)

	return r.call(entry, arguments...)
}
func (r *Runtime) call(entry uint64, arguments ...uint64) (uint64, error) {
	var err error

	for i := 0; i < 8; i++ {
		var v uint64
		if i < len(arguments) {
			v = arguments[i]
		}

		if e := r.engine.RegWrite(unicorn.RegX0+i, v); e != nil {
			return 0, fmt.Errorf("write guest register: %w", e)
		}
	}

	sp := stackBase + stackSize - 4096
	for i := 8; i < len(arguments); i++ {
		r.write64(sp+uint64(i-8)*8, arguments[i])
	}

	_ = r.engine.RegWrite(unicorn.RegSP, sp)
	_ = r.engine.RegWrite(unicorn.RegLR, returnPC)
	r.fault = nil
	err = r.engine.StartBounded(entry, returnPC, 2*time.Minute, 20_000_000)

	if r.fault != nil {
		return 0, r.fault
	}

	pc, _ := r.engine.RegRead(unicorn.RegPC)
	if err != nil {
		lr, _ := r.engine.RegRead(unicorn.RegLR)

		return 0, fmt.Errorf("guest stopped at %#x from %#x: %w; recent=%v", pc, lr, err, r.trace)
	}

	if pc != returnPC {
		return 0, fmt.Errorf("instruction budget exhausted at %#x", pc)
	}

	value, err := r.engine.RegRead(unicorn.RegX0)
	if err != nil {
		return 0, fmt.Errorf("read guest return register: %w", err)
	}

	return value, nil
}
func (r *Runtime) step(address uint64, _ uint32) {
	defer func() {
		if p := recover(); p != nil {
			r.fault = fmt.Errorf("guest bridge at %#x: %w; recent=%v", address, recoveredError("runtime", p), r.trace)
			_ = r.engine.Stop()
		}
	}()

	if callback := r.callbacks[address]; callback != nil {
		callback()

		return
	}

	if name := r.entries[address]; name != "" {
		r.trace = append(r.trace, name)
		if len(r.trace) > 12 {
			r.trace = r.trace[len(r.trace)-12:]
		}
	}

	call, ok := r.calls[address]
	if !ok {
		allowed := false

		for _, region := range r.codeRanges {
			if address >= region[0] && address < region[1] {
				allowed = true

				break
			}
		}

		if !allowed {
			panic(fmt.Sprintf("execution outside the pinned image at %#x", address))
		}

		r.steps++
		if r.steps%4096 == 0 {
			if err := r.ctx.Err(); err != nil {
				panic(err)
			}
		}

		return
	}

	if call.Selector != "" {
		r.message(r.arg(0), call.Selector, false)

		return
	}

	r.symbol(strings.TrimLeft(call.Symbol, "_"))
}
func (r *Runtime) jump(entry, receiver uint64, selector string) {
	_ = r.engine.RegWrite(unicorn.RegX0, receiver)
	_ = r.engine.RegWrite(unicorn.RegX1, r.selector(selector))
	_ = r.engine.RegWrite(unicorn.RegPC, entry)
}

func (r *Runtime) ClassAddress(name string) uint64 {
	if c := r.classes[name]; c != nil {
		return c.Address
	}

	return 0
}

// Export copies a Foundation value out of the guest without exposing guest pointers.
func (r *Runtime) Export(address uint64) (any, error) {
	return r.export(address, map[uint64]bool{}, 0)
}
func (r *Runtime) export(address uint64, active map[uint64]bool, depth int) (any, error) {
	if address == 0 {
		return nil, nil
	}

	if depth > 32 || active[address] {
		return nil, errors.New("cyclic or excessively nested guest value")
	}

	obj := r.objects[address]
	if obj == nil {
		return nil, fmt.Errorf("unknown exported object %#x", address)
	}

	active[address] = true
	defer delete(active, address)

	switch v := obj.Value.(type) {
	case map[string]uint64:
		result := map[string]any{}

		for k, p := range v {
			item, e := r.export(p, active, depth+1)
			if e != nil {
				return nil, e
			}

			result[k] = item
		}

		return result, nil
	case []uint64:
		result := make([]any, len(v))

		for i, p := range v {
			item, e := r.export(p, active, depth+1)
			if e != nil {
				return nil, e
			}

			result[i] = item
		}

		return result, nil
	default:
		return v, nil
	}
}

func (r *Runtime) queuePending(block, queue uint64) {
	if r.pendingQueues == nil {
		r.pendingQueues = map[uint64]uint64{}
	}

	r.pendingQueues[block] = queue
	r.pending = append(r.pending, block)
}
func (r *Runtime) nextPending() uint64 {
	for i, block := range r.pending {
		if q := r.objects[r.pendingQueues[block]]; q != nil && q.Properties["suspendCount"] != 0 {
			continue
		}

		r.pending = append(r.pending[:i], r.pending[i+1:]...)
		r.selectedQueue = r.pendingQueues[block]
		delete(r.pendingQueues, block)

		return block
	}

	return 0
}

func (r *Runtime) pendingCallback(block uint64, after func()) {
	previous := r.currentQueue
	r.currentQueue = r.selectedQueue
	r.callback(block, nil, func() { r.currentQueue = previous; after() })
}

func (r *Runtime) queueIsWithin(queue, target uint64) bool {
	for depth := 0; depth < 32; depth++ {
		if queue == target {
			return true
		}

		object := r.objects[queue]
		if object == nil {
			return false
		}

		queue = object.Properties["targetQueue"]
		if queue == 0 {
			return false
		}
	}

	panic("cyclic dispatch queue targets")
}

func recoveredError(stage string, value any) error {
	if cause, ok := value.(error); ok {
		return fmt.Errorf("%s: %w", stage, cause)
	}

	return fmt.Errorf("%s: %v", stage, value)
}
