package profilegen

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
)

type collector struct {
	v             *bootstrapVM
	p             *profileformat.Profile
	pages         map[uint64]bool
	metadataPages map[uint64]bool
	calls         map[uint64]profileformat.Call
	classes       map[uint64]string
	references    map[uint64]bool
	classNames    map[uint64]string
	neededClasses map[string]bool
}

func adrp(word uint32, pc uint64) uint64 {
	imm := int64((word>>5&0x7ffff)<<2 | word>>29&3)
	if imm&(1<<20) != 0 {
		imm -= 1 << 21
	}

	return uint64(int64(pc&^4095) + (imm << 12))
}

// addPage excludes objects represented by the host bridge. Their class layouts
// and values are exported separately; copying their runtime internals pulls in
// unrelated Objective-C registries and methods from other frameworks.
func (c *collector) addPage(a uint64) {
	if c.classNames[a] != "" {
		return
	}

	key := strconv.FormatUint(a, 10)
	if _, ok := c.p.Strings[key]; ok {
		return
	}

	if _, ok := c.p.Constants[key]; ok {
		return
	}

	c.rawPage(a)
}

// rawPage retains storage required by native Swift metadata even when its
// address also identifies a class handled by the Objective-C bridge.
func (c *collector) rawPage(a uint64) {
	if !inside(a, c.v.source.target) && !inside(a, c.v.source.code) && c.inCache(a) {
		c.pages[a&^4095] = true
	}

	if a >= bootHeap && a < bootHeap+c.v.cursor {
		c.pages[a&^4095] = true
	}
}

// reference records an actual address operand or pointer slot. Object graphs
// start at these roots, never at incidental pointers elsewhere in a copied page.
func (c *collector) reference(a uint64) {
	for !c.references[a] && c.inCache(a) && !inside(a, c.v.source.code) {
		c.references[a] = true
		if name := c.classNames[a]; name != "" {
			c.neededClasses[name] = true

			return
		}

		if c.recordString(a) {
			return
		}

		c.constant(a, 0)

		if _, ok := c.p.Constants[strconv.FormatUint(a, 10)]; ok {
			return
		}

		c.addPage(a)
		// An imported global can add multiple indirections before the object.
		// Follow that chain, rather than scanning neighboring pointer slots.
		a = c.v.source.u64(a)
	}

	c.addPage(a)
}

func (c *collector) inCache(a uint64) bool {
	return c.v.source.cache.Contains(a, 32)
}
func (c *collector) metadata(a uint64) {
	if a != 0 {
		c.rawPage(a)
		c.reference(a)
		c.metadataPages[a&^4095] = true
	}
}
func (c *collector) resolve(a uint64) {
	s := c.v.source
	if inside(a, s.target) {
		return
	}

	if _, ok := c.calls[a]; ok {
		return
	}

	b := s.read(a, 16)

	w0, w1, w2 := binary.LittleEndian.Uint32(b), binary.LittleEndian.Uint32(b[4:]), binary.LittleEndian.Uint32(b[8:])
	if w0&0x9f000000 == 0x90000000 {
		ptr := adrp(w0, a)

		switch w1 & 0xffc00000 {
		case 0x91000000:
			ptr += uint64(w1 >> 10 & 0xfff)
		case 0xf9400000:
			ptr = s.u64(ptr + uint64(w1>>10&0xfff)*8)
		default:
			ptr = 0
		}

		if ptr != 0 && w0&31 == 1 {
			c.calls[a] = profileformat.Call{Address: a, Selector: s.cstring(ptr), SelectorAddress: ptr}

			return
		}

		if ptr != 0 && w0&31 == 17 && w2&0xffc00000 == 0xf9400000 {
			ptr = s.u64(ptr)
			name := s.symbol(ptr)

			if name == "" {
				name = fmt.Sprintf("unresolved_%x", ptr)
			}

			c.calls[a] = profileformat.Call{Address: a, Symbol: canonicalSymbol(name)}

			return
		}
	}

	if name := s.symbol(a); name != "" {
		c.calls[a] = profileformat.Call{Address: a, Symbol: canonicalSymbol(name)}
	}
}
func (c *collector) constant(a uint64, depth int) {
	if a == 0 || depth > 16 || !c.inCache(a) {
		return
	}

	key := strconv.FormatUint(a, 10)
	if _, ok := c.p.Constants[key]; ok {
		return
	}

	s := c.v.source
	name := c.classes[s.u64(a)]
	item := profileformat.Constant{}

	switch name {
	case "__NSCFConstantString", "NSConstantString":
		item.Kind = "NSString"
		item.Text, _ = c.stringValue(a)
	case "NSConstantIntegerNumber":
		item.Kind = "NSNumber"
		item.Number = int64(s.u64(a + 16))
	case "__NSCFBoolean":
		item.Kind = "NSNumber"
		item.IsBoolean = true
		item.Boolean = a == s.u64(s.exports["_kCFBooleanTrue"])

		if item.Boolean {
			item.Number = 1
		}
	case "__NSDictionary0":
		item.Kind = "NSDictionary"
		item.Entries = map[string]uint64{}
	case "__NSArray0":
		item.Kind = "NSArray"
		item.Items = []uint64{}
	case "NSConstantDictionary":
		item.Kind = "NSDictionary"
		item.Entries = map[string]uint64{}
		c.p.Constants[key] = item

		n, keys, values := s.u64(a+16), s.u64(a+24), s.u64(a+32)
		if n > 1<<20 {
			panic("constant dictionary too large")
		}

		for i := uint64(0); i < n; i++ {
			k, val := s.u64(keys+i*8), s.u64(values+i*8)
			c.constant(val, depth+1)

			if text, ok := c.stringValue(k); ok {
				item.Entries[text] = val
			}
		}
	case "NSConstantArray":
		item.Kind = "NSArray"
		item.Items = []uint64{}
		c.p.Constants[key] = item

		n, values := s.u64(a+8), s.u64(a+16)
		if n > 1<<20 {
			panic("constant array too large")
		}

		for i := uint64(0); i < n; i++ {
			val := s.u64(values + i*8)
			c.constant(val, depth+1)
			item.Items = append(item.Items, val)
		}
	default:
		return
	}

	c.p.Constants[key] = item
}
func (v *bootstrapVM) collect(p *profileformat.Profile, classes []profileformat.Class) []byte {
	s := v.source
	p.Classes = classes
	p.UUID = s.images[amsImage].UUID().String()
	p.PluginUUID = s.images[authImage].UUID().String()
	p.Strings = map[string]string{}
	p.Constants = map[string]profileformat.Constant{}

	c := &collector{v: v, p: p, pages: map[uint64]bool{}, metadataPages: map[uint64]bool{}, calls: map[uint64]profileformat.Call{}, classes: map[uint64]string{}, references: map[uint64]bool{}, classNames: map[uint64]string{}, neededClasses: map[string]bool{}}
	for _, class := range classes {
		c.classes[class.Address] = class.Name
		c.classNames[class.Address] = class.Name
		c.classNames[class.MetaAddress] = class.Name
	}

	for _, name := range []string{amsImage, authImage} {
		for _, sec := range s.images[name].Sections {
			if sec.Flags.IsPureInstructions() || sec.Flags.IsSomeInstructions() {
				b := s.read(sec.Addr, sec.Size)
				dataReferences(b, sec.Addr, c.reference)

				for off := uint64(0); off+4 <= sec.Size; off += 4 {
					word := binary.LittleEndian.Uint32(b[off:])
					pc := sec.Addr + off

					if word&0x7c000000 == 0x14000000 {
						imm := int64(word & 0x3ffffff)
						if imm&(1<<25) != 0 {
							imm -= 1 << 26
						}

						c.resolve(uint64(int64(pc) + imm*4))
					}
				}
			} else if sec.Name != "__bss" && sec.Name != "__common" {
				b := s.read(sec.Addr, sec.Size)
				if sec.Name == "__cstring" || sec.Name == "__objc_classname" {
					for _, name := range strings.Split(string(b), "\x00") {
						c.neededClasses[className(name)] = true
					}
				}

				for off := uint64(0); off+8 <= sec.Size; off += 8 {
					if sec.Name == "__cfstring" && off%32 == 0 {
						c.recordString(sec.Addr + off)
					}

					switch sec.Name {
					case "__got", "__auth_got", "__objc_classrefs", "__objc_superrefs", "__objc_selrefs", "__la_symbol_ptr", "__nl_symbol_ptr":
						c.reference(binary.LittleEndian.Uint64(b[off:]))
					}

					name := c.classes[binary.LittleEndian.Uint64(b[off:])]
					if name == "NSConstantDictionary" || name == "NSConstantArray" || name == "NSConstantIntegerNumber" {
						c.constant(sec.Addr+off, 0)
					}
				}
			}
		}
	}

	for _, call := range c.calls {
		if a := s.exports["_"+call.Symbol]; a != 0 && !inside(a, s.target) {
			c.calls[a] = profileformat.Call{Address: a, Symbol: canonicalSymbol(call.Symbol)}
		}
	}

	for _, name := range []string{"___chkstk_darwin", "gmtime_r", "localtime_r"} {
		if a := s.exports["_"+name]; a != 0 {
			c.calls[a] = profileformat.Call{Address: a, Symbol: canonicalSymbol(name)}
		}
	}

	for _, m := range []map[string]uint64{p.SwiftTypes, p.SwiftAccessors, p.SwiftSingletons, p.SwiftForeign, p.ObjCMetadata} {
		for _, a := range m {
			c.metadata(a)
			c.metadata(v.u64(a - 8))
		}
	}

	for _, a := range p.Bridges {
		c.metadata(a)
	}

	c.metadata(v.u64(p.Bridges["anyHashableStringMetadata"] - 8))

	for _, a := range p.SwiftWitnesses {
		c.metadata(a)
	}

	for _, a := range p.SwiftGlobals {
		c.metadata(a)
		c.metadata(v.u64(a))
	}

	for _, layout := range p.SwiftDictionaries {
		c.metadata(layout.Metadata)
	}

	for name, a := range p.Symbols {
		c.reference(a)

		if strings.HasSuffix(name, "N") {
			off := uint64(8)
			if name == "$sypN" {
				off = 0
			}

			c.metadata(v.u64(a - off))
		}
	}
	// Preserve initialized metadata inside the executable images, including Swift
	// field offsets. Executable pages are supplied separately by the image loader.
	for page := range v.mapped {
		if inside(page, s.target) && !inside(page, s.code) {
			b := v.read(page, 4096)
			original := s.read(page, 4096)

			for off := 0; off < 4096; off += 8 {
				if binary.LittleEndian.Uint64(b[off:]) != binary.LittleEndian.Uint64(original[off:]) {
					p.MetadataPatches = append(p.MetadataPatches, profileformat.MetadataPatch{Address: page + uint64(off), Data: append([]byte(nil), b[off:off+8]...)})
				}
			}
		}
	}

	sort.Slice(p.MetadataPatches, func(i, j int) bool { return p.MetadataPatches[i].Address < p.MetadataPatches[j].Address })

	var pages []byte

	done := map[uint64]bool{}

	for pass := 0; pass < 2; pass++ {
		addresses := make([]uint64, 0, len(c.pages))
		for a := range c.pages {
			addresses = append(addresses, a)
		}

		sort.Slice(addresses, func(i, j int) bool { return addresses[i] < addresses[j] })

		for _, a := range addresses {
			if done[a] {
				continue
			}

			done[a] = true

			b := v.snapshotPage(a)
			for off := 0; off < 4096; off += 8 {
				ptr := binary.LittleEndian.Uint64(b[off:])
				if name := c.classNames[ptr]; name != "" {
					c.neededClasses[name] = true
				}

				if c.inCache(ptr) {
					if c.metadataPages[a] && !inside(ptr, s.target) && inside(ptr, s.code) {
						if name := s.symbol(ptr); name != "" {
							c.calls[ptr] = profileformat.Call{Address: ptr, Symbol: canonicalSymbol(name)}
						}
					}
				}

				// Swift metadata contains dynamically indexed tables. Preserve
				// their pointer dependencies; ordinary data pages are rooted at
				// the exact operands above, not every neighboring pointer.
				if pass == 0 && c.metadataPages[a] {
					c.addPage(ptr)
				}
			}

			p.Pages = append(p.Pages, profileformat.Page{Address: a, Offset: uint64(len(pages))})
			pages = append(pages, b...)
		}
	}

	for _, name := range []string{"_kCFBooleanTrue", "_kCFBooleanFalse"} {
		if a := s.exports[name]; a != 0 {
			c.constant(s.u64(a), 0)
		}
	}

	for _, call := range c.calls {
		p.Calls = append(p.Calls, call)
	}

	sort.Slice(p.Calls, func(i, j int) bool { return p.Calls[i].Address < p.Calls[j].Address })

	for name := range p.ObjCMetadata {
		c.neededClasses[name] = true
	}

	if len(p.Strings) != 0 {
		c.neededClasses["NSString"] = true
	}

	for _, constant := range p.Constants {
		c.neededClasses[constant.Kind] = true
	}

	p.Classes = reachableClasses(classes, c.neededClasses)

	return pages
}

// Snapshot untouched pages directly from the binary. Mapping thousands of
// read-only pages into Unicorn here would make export needlessly quadratic.
func (v *bootstrapVM) snapshotPage(a uint64) []byte {
	if !v.mapped[a] && (a < bootHeap || a >= bootHeap+v.cursor) {
		return v.source.read(a, 4096)
	}

	b := v.read(a, 4096)

	if v.source.cache.Contains(a, 4096) {
		for off := uint64(0); off < 4096; off += 4 {
			if inside(a+off, v.source.code) {
				copy(b[off:off+4], v.source.read(a+off, 4))
			}
		}
	}

	return b
}

func canonicalSymbol(name string) string {
	switch name {
	case "swift_release_preservemost":
		return "swift_release"
	case "swift_retain_preservemost":
		return "swift_retain"
	case "dispatch_channel_async":
		return "dispatch_async"
	case "dispatch_queue_create_with_target$V2":
		return "dispatch_queue_create_with_target"
	case "exp2l":
		return "exp2"
	case "_platform_bzero":
		return "__bzero"
	case "objc_release_x0":
		return "objc_release"
	case "objc_retain_x0":
		return "objc_retain"
	case "powl":
		return "pow"
	case "dispatch_assert_queue$V2":
		return "dispatch_assert_queue"
	case "dispatch_assert_queue_not$V2":
		return "dispatch_assert_queue_not"
	}

	return name
}

func (c *collector) stringValue(a uint64) (string, bool) {
	if a == 0 || !c.inCache(a) {
		return "", false
	}

	s := c.v.source

	name := c.classes[s.u64(a)]
	if name != "__NSCFConstantString" && name != "NSConstantString" {
		return "", false
	}

	ptr, n := s.u64(a+16), s.u64(a+24)
	if n > 1<<20 {
		panic("constant string too large")
	}

	if s.u32(a+8)&0x10 != 0 {
		b := s.read(ptr, n*2)
		u := make([]uint16, n)

		for i := range u {
			u[i] = binary.LittleEndian.Uint16(b[i*2:])
		}

		return string(utf16.Decode(u)), true
	}

	return string(s.read(ptr, n)), true
}
func (c *collector) recordString(a uint64) bool {
	key := strconv.FormatUint(a, 10)
	if _, ok := c.p.Strings[key]; ok {
		return true
	}

	if text, ok := c.stringValue(a); ok {
		c.p.Strings[key] = text

		return true
	}

	return false
}
