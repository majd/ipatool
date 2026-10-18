package profilegen

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/majd/ipatool/v2/internal/sap/unicorn"
	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
)

func (v *bootstrapVM) swiftMetadata(req *swiftRequests) (*profileformat.Profile, error) {
	p := v.metadata
	if p == nil {
		p = newMetadataProfile()
		v.metadata = p
	}

	key := func(a uint64) string { return strconv.FormatUint(a, 10) }

	for _, name := range []string{"$ss11AnyHashableVN", "$sSSN", "$sSiN", "$sSbN", "$sSdN", "$sypN", "_swift_stdlib_Hashing_parameters", "_swiftEmptyDictionarySingleton", "_swiftEmptyArrayStorage", "_swiftEmptySetSingleton"} {
		address := v.source.exports["_"+name]
		if address == 0 {
			return nil, fmt.Errorf("missing Swift data symbol %s", name)
		}

		p.Symbols[name] = address
	}

	for _, r := range req.Types {
		address, _, err := v.call("swift_getTypeByMangledNameInContext2", r.Address, r.Length, 0, 0)
		if err != nil {
			return nil, fmt.Errorf("initialize Swift type %#x/%d: %w", r.Address, r.Length, err)
		}

		if address == 0 {
			return nil, fmt.Errorf("unresolved Swift type %#x", r.Address)
		}

		p.SwiftTypes[key(r.Address)] = address
	}

	for _, r := range req.Nominals {
		address, state, err := v.callAddress(r.Address, 0)
		if err != nil {
			return nil, fmt.Errorf("initialize Swift nominal %#x: %w", r.Descriptor, err)
		}

		if address == 0 || state != 0 {
			return nil, fmt.Errorf("incomplete Swift nominal %#x", r.Descriptor)
		}
	}

	for _, r := range req.Globals {
		address, _, err := v.callAddress(r.Address)
		if err != nil {
			return nil, fmt.Errorf("decode Swift metadata requests: %w", err)
		}

		if address == 0 {
			return nil, fmt.Errorf("unresolved Swift global %s", r.Name)
		}

		p.SwiftGlobals[r.Name] = address
	}

	boxName, protocolName := "s20_ConcreteHashableBoxVySSG", "s15_AnyHashableBox_p"

	box, _, err := v.call("swift_getTypeByMangledNameInContext2", v.cstring(boxName), uint64(len(boxName)), 0, 0)
	if err != nil {
		return nil, fmt.Errorf("decode Swift metadata requests: %w", err)
	}

	proto, _, err := v.call("swift_getTypeByMangledNameInContext2", v.cstring(protocolName), uint64(len(protocolName)), 0, 0)
	if err != nil {
		return nil, fmt.Errorf("decode Swift metadata requests: %w", err)
	}

	witness, _, err := v.call("swift_conformsToProtocol", box, v.u64(proto+16))
	if err != nil {
		return nil, fmt.Errorf("decode Swift metadata requests: %w", err)
	}

	if box == 0 || witness == 0 {
		return nil, fmt.Errorf("missing AnyHashable bridge")
	}

	p.Bridges["anyHashableStringMetadata"] = box
	p.Bridges["anyHashableStringWitness"] = witness

	for _, name := range []string{"NSString", "NSNumber", "NSData", "NSDictionary", "NSArray", "NSObject", "NSNull"} {
		var class uint64

		for _, c := range v.source.classes {
			if c.Name == name {
				class = c.Address

				break
			}
		}

		address, _, err := v.call("swift_getObjCClassMetadata", class)
		if err != nil {
			return nil, fmt.Errorf("decode Swift metadata requests: %w", err)
		}

		if address == 0 {
			return nil, fmt.Errorf("missing ObjC metadata %s", name)
		}

		p.ObjCMetadata[name] = address
	}

	for _, name := range req.Accessors {
		address, state, err := v.call(name, 0)
		if err != nil {
			return nil, fmt.Errorf("decode Swift metadata requests: %w", err)
		}

		if address == 0 || state != 0 {
			return nil, fmt.Errorf("incomplete accessor %s", name)
		}

		p.SwiftAccessors[name] = address
	}

	dictionaryDescriptor := v.source.exports["_$ss17_NativeDictionaryVMn"]
	if dictionaryDescriptor == 0 {
		return nil, fmt.Errorf("missing Swift native dictionary descriptor")
	}

	for _, r := range req.Types {
		if v.u64(p.SwiftTypes[key(r.Address)]+8) == dictionaryDescriptor {
			metadata := p.SwiftTypes[key(r.Address)]
			slot := v.alloc(8)
			v.put(slot, p.Symbols["_swiftEmptyDictionarySingleton"])
			v.writeRegister(unicorn.RegX0+20, slot)

			_, _, err := v.call("$ss17_NativeDictionaryV12ensureUnique02isD08capacityS2b_SitF", 0, 512, metadata)
			if err != nil {
				return nil, fmt.Errorf("decode Swift metadata requests: %w", err)
			}

			storage := v.u64(slot)
			keyType, valueType := v.u64(metadata+16), v.u64(metadata+24)
			p.SwiftDictionaries[key(metadata)] = profileformat.DictionaryLayout{Metadata: v.u64(storage), KeyStride: v.u64(v.u64(keyType-8) + 72), ValueStride: v.u64(v.u64(valueType-8) + 72)}
		}
	}

	if err := v.prepareWitnesses(req); err != nil {
		return nil, err
	}

	return p, nil
}

func newMetadataProfile() *profileformat.Profile {
	return &profileformat.Profile{SwiftTypes: map[string]uint64{}, SwiftAccessors: map[string]uint64{}, SwiftWitnesses: map[string]uint64{}, SwiftGlobals: map[string]uint64{}, SwiftForeign: map[string]uint64{}, SwiftSingletons: map[string]uint64{}, SwiftDictionaries: map[string]profileformat.DictionaryLayout{}, ObjCMetadata: map[string]uint64{}, Bridges: map[string]uint64{}, Symbols: map[string]uint64{}}
}

func (v *bootstrapVM) prepareWitnesses(req *swiftRequests) error {
	byDescriptor := map[uint64]map[uint64]bool{}

	add := func(a uint64) {
		kind := v.u64(a)
		d := uint64(0)

		if kind == 0x200 || kind == 0x201 {
			d = v.u64(a + 8)
		} else if kind > 0x7ff {
			d = v.u64(a + 64)
		}

		if d != 0 {
			if byDescriptor[d] == nil {
				byDescriptor[d] = map[uint64]bool{}
			}

			byDescriptor[d][a] = true
		}
	}
	for _, a := range v.metadata.SwiftTypes {
		add(a)
	}

	for _, a := range v.metadata.SwiftAccessors {
		add(a)
	}

	for _, a := range v.metadata.SwiftSingletons {
		add(a)
	}

	for _, a := range v.metadata.SwiftForeign {
		add(a)
	}

	for _, r := range req.Nominals {
		a, _, err := v.callAddress(r.Address, 0)
		if err != nil {
			return err
		}

		add(a)
	}

	for _, conformance := range req.Conformances {
		flags := v.source.u32(conformance + 12)
		// Conditional conformances require caller-supplied evidence and cannot be
		// initialized from a nominal descriptor alone.
		if flags&0xff00 != 0 || flags&0xff000000 != 0 {
			continue
		}

		kind := (flags >> 3) & 7
		if kind > 1 {
			continue
		}

		d := v.source.relative(conformance + 4)
		if kind == 1 {
			d = v.source.u64(d)
		}

		if v.source.u32(d)&0x80 == 0 && len(byDescriptor[d]) == 0 && v.source.u32(d+12) != 0 {
			a, state, err := v.callAddress(v.source.relative(d+12), 0)
			if err != nil {
				return fmt.Errorf("initialize conformance type %#x: %w", d, err)
			}

			if a == 0 || state != 0 {
				return fmt.Errorf("incomplete conformance type %#x", d)
			}

			add(a)
		}

		var instances []uint64
		for a := range byDescriptor[d] {
			instances = append(instances, a)
		}

		sort.Slice(instances, func(i, j int) bool { return instances[i] < instances[j] })

		for _, a := range instances {
			witness, _, err := v.call("swift_getWitnessTable", conformance, a, 0)
			if err != nil {
				return fmt.Errorf("initialize witness %#x for %#x: %w", conformance, a, err)
			}

			if witness == 0 {
				return fmt.Errorf("unresolved witness %#x for %#x", conformance, a)
			}
		}
	}

	return nil
}
