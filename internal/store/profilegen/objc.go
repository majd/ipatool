package profilegen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
)

func (s *source) objcClass(address uint64) *profileformat.Class {
	if address == 0 {
		return nil
	}

	if c := s.classes[address]; c != nil {
		return c
	}

	data := s.u64(address+32) &^ 7
	c := &profileformat.Class{Address: address, MetaAddress: s.u64(address), Ivars: map[string]uint64{}}
	s.classes[address] = c
	c.Name = className(s.cstring(s.u64(data + 24)))
	c.Size = (uint64(s.u32(data+8)) + 7) &^ 7

	if c.Name == "" {
		panic(fmt.Sprintf("class at %#x has no name (ro %#x)", address, data))
	}

	if superclass := s.objcClass(s.u64(address + 8)); superclass != nil {
		c.Super = superclass.Name
	}

	c.Methods = append(c.Methods, s.methods(s.u64(data+32), false)...)
	metaData := s.u64(c.MetaAddress+32) &^ 7
	c.Methods = append(c.Methods, s.methods(s.u64(metaData+32), true)...)

	if list := s.u64(data + 48); list != 0 {
		stride, count := s.u32(list), s.u32(list+4)
		if stride < 32 || count > 1<<16 {
			panic("invalid ivar list")
		}

		for i := uint32(0); i < count; i++ {
			entry := list + 8 + uint64(i)*uint64(stride)
			name := s.cstring(s.u64(entry + 8))

			offset := s.u64(entry)
			if offset != 0 {
				c.Ivars[name] = uint64(s.u32(offset))
			}
		}
	}

	return c
}
func (s *source) methods(list uint64, meta bool) []profileformat.Method {
	if list == 0 {
		return nil
	}

	if list&1 != 0 {
		list &^= 1

		count := s.u32(list + 4)
		if count > 1<<16 {
			panic("invalid attached method lists")
		}

		var result []profileformat.Method

		for i := uint32(0); i < count; i++ {
			entry := list + 8 + uint64(i)*8
			offset := int64(s.u64(entry)) >> 16
			result = append(result, s.methods(uint64(int64(entry)+offset), meta)...)
		}

		return result
	}

	flags, count := s.u32(list), s.u32(list+4)
	if count == 0 {
		return nil
	}

	stride := uint64(flags & 0xfffc)
	if stride < 12 || count > 1<<18 {
		panic(fmt.Sprintf("invalid method list %#x (%x,%d)", list, flags, count))
	}

	var result []profileformat.Method

	for i := uint32(0); i < count; i++ {
		entry := list + 8 + uint64(i)*stride

		var sel, types, imp uint64

		if flags&0x80000000 != 0 {
			if flags&0x40000000 != 0 {
				sel = s.selectorBase + uint64(s.u32(entry))
			} else {
				sel = s.u64(uint64(int64(entry) + int64(int32(s.u32(entry)))))
			}

			if flags&0x20000000 != 0 {
				types = s.selectorBase + uint64(s.u32(entry+4))
			} else {
				types = uint64(int64(entry+4) + int64(int32(s.u32(entry+4))))
			}

			imp = uint64(int64(entry+8) + int64(int32(s.u32(entry+8))))
		} else {
			sel = s.u64(entry)
			types = s.u64(entry + 8)
			imp = s.u64(entry + 16)
		}

		if inside(imp, s.target) {
			result = append(result, profileformat.Method{Selector: s.cstring(sel), Types: s.cstring(types), Address: imp, ClassMethod: meta})
		}
	}

	return result
}
func (s *source) objcClasses() []profileformat.Class {
	for _, name := range s.imageNames() {
		f := s.images[name]
		for _, sec := range f.Sections {
			if sec.Name == "__objc_classlist" {
				for off := uint64(0); off < sec.Size; off += 8 {
					s.objcClass(s.u64(sec.Addr + off))
				}
			}
		}
	}
	// Categories can install Store methods on Foundation classes.
	for _, name := range []string{amsImage, authImage} {
		for _, sec := range s.images[name].Sections {
			if sec.Name == "__objc_catlist" {
				for off := uint64(0); off < sec.Size; off += 8 {
					cat := s.u64(sec.Addr + off)

					class := s.objcClass(s.u64(cat + 8))
					if class == nil {
						panic("unresolved category class")
					}

					class.Methods = append(class.Methods, s.methods(s.u64(cat+16), false)...)
					class.Methods = append(class.Methods, s.methods(s.u64(cat+24), true)...)
				}
			}
		}
	}

	classes := make([]profileformat.Class, 0, len(s.classes))
	for _, c := range s.classes {
		seen := map[string]bool{}
		methods := c.Methods[:0]

		for _, m := range c.Methods {
			key := strconv.FormatBool(m.ClassMethod) + m.Selector
			if !seen[key] {
				seen[key] = true

				methods = append(methods, m)
			}
		}

		c.Methods = methods
		sort.Slice(c.Methods, func(i, j int) bool {
			a, b := c.Methods[i], c.Methods[j]
			if a.ClassMethod != b.ClassMethod {
				return !a.ClassMethod
			}

			return a.Selector < b.Selector
		})

		classes = append(classes, *c)
	}

	sort.Slice(classes, func(i, j int) bool { return classes[i].Name < classes[j].Name })

	return classes
}

// The ObjC runtime exposes public Swift class names in qualified form. Private
// and generic encodings remain mangled; their dynamic metadata is handled later.
func className(name string) string {
	if !strings.HasPrefix(name, "_TtC") {
		return name
	}

	value := strings.TrimPrefix(name, "_TtC")

	var parts []string

	if strings.HasPrefix(value, "s") {
		parts = append(parts, "Swift")
		value = value[1:]
	}

	for value != "" {
		i := 0
		for i < len(value) && value[i] >= '0' && value[i] <= '9' {
			i++
		}

		if i == 0 {
			return name
		}

		n, e := strconv.Atoi(value[:i])
		if e != nil || n > len(value)-i {
			return name
		}

		parts = append(parts, value[i:i+n])
		value = value[i+n:]
	}

	if len(parts) < 2 {
		return name
	}

	return strings.Join(parts, ".")
}
