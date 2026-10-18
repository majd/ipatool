package profilegen

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
	"sort"
	"strconv"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
)

type profileMemory struct {
	profile *profileformat.Profile
	pages   []byte
	index   map[uint64]uint64
	cache   *assets.StoreCache
}

func memory(p *profileformat.Profile, pages []byte, cache *assets.StoreCache) *profileMemory {
	m := &profileMemory{profile: p, pages: pages, index: map[uint64]uint64{}, cache: cache}
	for _, page := range p.Pages {
		m.index[page.Address] = page.Offset
	}

	return m
}
func (m *profileMemory) read(a, n uint64) []byte {
	b := make([]byte, n)

	for i := uint64(0); i < n; {
		page := (a + i) &^ 4095
		off := (a + i) & 4095
		size := min(n-i, 4096-off)

		var data []byte

		if pos, ok := m.index[page]; ok {
			if pos > uint64(len(m.pages)) || uint64(len(m.pages))-pos < 4096 {
				panic("invalid validation page index")
			}

			data = m.pages[pos : pos+4096]
		} else {
			var err error

			data, err = m.cache.Page(page)
			if err != nil {
				panic(err)
			}
		}

		copy(b[i:i+size], data[off:off+size])
		i += size
	}

	for _, patch := range m.profile.MetadataPatches {
		start, end := max(a, patch.Address), min(a+n, patch.Address+uint64(len(patch.Data)))
		if start < end {
			copy(b[start-a:end-a], patch.Data[start-patch.Address:end-patch.Address])
		}
	}

	return b
}
func (m *profileMemory) u64(a uint64) uint64 { return binary.LittleEndian.Uint64(m.read(a, 8)) }

// Validate checks ABI-relevant values against an independently exported native
// profile. Heap addresses and unrelated dependency closures may legitimately
// differ; object layouts, method entry points, and Swift value layouts may not.
func (r *Result) Validate(cache *assets.StoreCache, reference *profileformat.Profile, pages []byte) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("validate Store profile: %v", failure)
		}
	}()

	generated := r.Profile
	if generated.UUID != reference.UUID || generated.PluginUUID != reference.PluginUUID {
		return fmt.Errorf("reference uses different Store binaries")
	}

	var targets [][2]uint64

	for _, name := range []string{amsImage, authImage} {
		image, e := cache.OpenImage(name)
		if e != nil {
			return fmt.Errorf("validate Store profile: %w", e)
		}

		for _, seg := range image.Segments() {
			if seg.Name != "__LINKEDIT" {
				targets = append(targets, [2]uint64{seg.Addr, seg.Addr + seg.Memsz})
			}
		}

		_ = image.Close()
	}

	sort.Slice(targets, func(i, j int) bool { return targets[i][0] < targets[j][0] })

	classes := map[string]profileformat.Class{}
	for _, c := range generated.Classes {
		classes[c.Name] = c
	}

	for _, expected := range reference.Classes {
		if len(expected.Methods) == 0 {
			continue
		}

		actual, ok := classes[expected.Name]
		if !ok || actual.Address != expected.Address || actual.MetaAddress != expected.MetaAddress || actual.Size != expected.Size || actual.Super != expected.Super || !reflect.DeepEqual(actual.Ivars, expected.Ivars) {
			return fmt.Errorf("class layout mismatch: %s", expected.Name)
		}

		methods := map[string]profileformat.Method{}
		for _, m := range actual.Methods {
			methods[fmt.Sprintf("%t:%s", m.ClassMethod, m.Selector)] = m
		}

		expectedMethods := map[string]profileformat.Method{}
		for _, m := range expected.Methods {
			expectedMethods[fmt.Sprintf("%t:%s", m.ClassMethod, m.Selector)] = m
		}

		for _, m := range expectedMethods {
			if methods[fmt.Sprintf("%t:%s", m.ClassMethod, m.Selector)] != m {
				return fmt.Errorf("method mismatch: %s %s", expected.Name, m.Selector)
			}
		}
	}

	if !reflect.DeepEqual(generated.Features, reference.Features) {
		return fmt.Errorf("feature defaults differ from native profile")
	}

	for key, value := range reference.Strings {
		address, e := strconv.ParseUint(key, 10, 64)
		if e != nil {
			return fmt.Errorf("validate Store profile: %w", e)
		}

		if _, ok := generated.Strings[key]; !ok && inside(address, targets) {
			return fmt.Errorf("missing Store string at %s", key)
		}

		if actual, ok := generated.Strings[key]; ok && actual != value {
			return fmt.Errorf("constant string mismatch at %s", key)
		}
	}

	for key, value := range reference.Constants {
		address, e := strconv.ParseUint(key, 10, 64)
		if e != nil {
			return fmt.Errorf("validate Store profile: %w", e)
		}

		if _, ok := generated.Constants[key]; !ok && inside(address, targets) {
			return fmt.Errorf("missing Store constant at %s", key)
		}

		if actual, ok := generated.Constants[key]; ok && !equalConstant(actual, value) {
			return fmt.Errorf("constant object mismatch at %s", key)
		}
	}

	calls := map[uint64]profileformat.Call{}
	for _, call := range generated.Calls {
		calls[call.Address] = call
	}

	for _, call := range reference.Calls {
		if call.Selector != "" {
			actual := calls[call.Address]
			if actual.Selector != call.Selector || actual.SelectorAddress != call.SelectorAddress {
				return fmt.Errorf("selector bridge mismatch at %#x", call.Address)
			}
		}
	}

	old, current := memory(reference, pages, cache), memory(generated, r.Pages, cache)

	for _, pair := range [][2]map[string]uint64{{reference.SwiftTypes, generated.SwiftTypes}, {reference.SwiftAccessors, generated.SwiftAccessors}, {reference.SwiftSingletons, generated.SwiftSingletons}, {reference.SwiftForeign, generated.SwiftForeign}, {reference.ObjCMetadata, generated.ObjCMetadata}} {
		for key, a := range pair[0] {
			b := pair[1][key]
			if b == 0 {
				return fmt.Errorf("missing Swift metadata %s", key)
			}

			ka, kb := old.u64(a), current.u64(b)
			if ka > 0x7ff {
				ka = 0
			}

			if kb > 0x7ff {
				kb = 0
			}

			if ka != kb {
				return fmt.Errorf("swift metadata kind mismatch: %s", key)
			}

			nativeLayout := old.read(old.u64(a-8)+64, 24)
			// The old exporter omitted initialized in-image value-witness tables.
			// Compare every available native layout; incomplete uncaptured tables
			// carry no reference evidence.
			if binary.LittleEndian.Uint32(nativeLayout[16:])&0x400000 == 0 && !bytes.Equal(nativeLayout, current.read(current.u64(b-8)+64, 24)) {
				return fmt.Errorf("swift value layout mismatch: %s native=%x generated=%x", key, nativeLayout, current.read(current.u64(b-8)+64, 24))
			}

			switch ka {
			case 0:
				if old.u64(a+64) != current.u64(b+64) || !bytes.Equal(old.read(a+48, 8), current.read(b+48, 8)) {
					return fmt.Errorf("swift class layout mismatch: %s", key)
				}
			case 0x200, 0x201, 0x305:
				if old.u64(a+8) != current.u64(b+8) {
					return fmt.Errorf("swift type identity mismatch: %s", key)
				}
			}
		}
	}

	for key, a := range reference.SwiftTypes {
		if layout, ok := reference.SwiftDictionaries[strconv.FormatUint(a, 10)]; ok {
			other, ok := generated.SwiftDictionaries[strconv.FormatUint(generated.SwiftTypes[key], 10)]
			if !ok || other.KeyStride != layout.KeyStride || other.ValueStride != layout.ValueStride {
				return fmt.Errorf("swift dictionary layout mismatch: %s", key)
			}
		}
	}

	for key, a := range reference.SwiftWitnesses {
		if !equalWitness(old, current, a, generated.SwiftWitnesses[key], map[[2]uint64]bool{}) {
			return fmt.Errorf("swift witness methods mismatch: %s", key)
		}
	}

	if !reflect.DeepEqual(reference.Symbols, generated.Symbols) || !reflect.DeepEqual(reference.Bridges, generated.Bridges) {
		return fmt.Errorf("swift bridge identities differ")
	}

	return nil
}
func equalConstant(a, b profileformat.Constant) bool {
	if a.Kind != b.Kind || a.Text != b.Text || a.Number != b.Number || a.IsBoolean != b.IsBoolean || a.Boolean != b.Boolean || len(a.Items) != len(b.Items) || len(a.Entries) != len(b.Entries) {
		return false
	}

	for i, v := range a.Items {
		if b.Items[i] != v {
			return false
		}
	}

	for k, v := range a.Entries {
		if other, ok := b.Entries[k]; !ok || other != v {
			return false
		}
	}

	return true
}

func equalWitness(a, b *profileMemory, left, right uint64, seen map[[2]uint64]bool) bool {
	if left == 0 || right == 0 {
		return false
	}

	pair := [2]uint64{left, right}
	if seen[pair] {
		return true
	}

	seen[pair] = true

	desc := a.u64(left)
	if desc != b.u64(right) {
		return false
	}

	relative := int32(binary.LittleEndian.Uint32(a.read(desc, 4)))
	protocol := uint64(int64(desc) + int64(relative&^1))

	if relative&1 != 0 {
		protocol = a.u64(protocol)
	}

	count := binary.LittleEndian.Uint32(a.read(protocol+16, 4))
	if count > 1024 {
		panic("invalid witness requirement count")
	}

	signatures := binary.LittleEndian.Uint32(a.read(protocol+12, 4))
	if signatures > 1024 {
		panic("invalid witness signature count")
	}

	for i := uint32(0); i < count; i++ {
		x, y := a.u64(left+8+uint64(i)*8), b.u64(right+8+uint64(i)*8)
		if x == y {
			continue
		}

		flags := binary.LittleEndian.Uint32(a.read(protocol+24+uint64(signatures)*12+uint64(i)*8, 4))
		if flags&15 != 0 || !equalWitness(a, b, x, y, seen) {
			return false
		}
	}

	return true
}
