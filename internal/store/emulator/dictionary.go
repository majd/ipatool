package emulator

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// A bounded implementation of Swift native dictionary storage. Metadata and
// strides come from the matching runtime. References live for the VM lifetime,
// so copying these observed pointer-only key/value layouts is bitwise safe.
func (r *Runtime) ensureUniqueDictionary(self uint64, unique bool, capacity, metadata uint64) {
	old := r.read64(self)

	oldCapacity := r.read64(old + 24)
	if unique && capacity <= oldCapacity {
		r.set(0)

		return
	}

	layout, ok := r.profile.SwiftDictionaries[strconv.FormatUint(metadata, 10)]
	if !ok {
		panic(fmt.Sprintf("unprepared dictionary metadata %#x", metadata))
	}

	if (layout.KeyStride != 16 && layout.KeyStride != 32 && layout.KeyStride != 40) || (layout.ValueStride != 8 && layout.ValueStride != 16 && layout.ValueStride != 32) {
		panic("unsupported dictionary copy layout")
	}

	const buckets = uint64(1024)

	const storageCapacity = buckets * 3 / 4
	if capacity > storageCapacity {
		panic("dictionary exceeds runtime capacity")
	}

	count := r.read64(old + 16)
	if count != 0 && oldCapacity != storageCapacity {
		panic("dictionary resizing requires rehash")
	}

	headerSize := 64 + buckets/8
	size := headerSize + buckets*(layout.KeyStride+layout.ValueStride)
	storage := r.alloc(size)
	r.write64(storage, layout.Metadata)
	r.write64(storage+8, 3)
	r.write64(storage+16, count)
	r.write64(storage+24, storageCapacity)
	r.write64(storage+32, 10)
	r.write64(storage+40, r.read64(old+40))
	keys := storage + headerSize
	values := keys + buckets*layout.KeyStride
	r.write64(storage+48, keys)
	r.write64(storage+56, values)

	if count != 0 {
		for _, span := range [][3]uint64{{storage + 64, old + 64, buckets / 8}, {keys, r.read64(old + 48), buckets * layout.KeyStride}, {values, r.read64(old + 56), buckets * layout.ValueStride}} {
			data, err := r.engine.MemRead(span[1], span[2])
			if err != nil {
				panic(err)
			}

			if err = r.MemWrite(span[0], data); err != nil {
				panic(err)
			}
		}
	}

	r.write64(self, storage)
	r.set(boolValue(oldCapacity != storageCapacity))
}

func (r *Runtime) bridgeDictionaryToSwift(object, keyType, valueType uint64) uint64 {
	empty := r.profile.Symbols["_swiftEmptyDictionarySingleton"]
	if object == 0 {
		return empty
	}

	values := r.objects[object].Value.(map[string]uint64)
	if len(values) == 0 {
		return empty
	}

	typeRef, keyStride := uint64(0x1a2e1d5d6), uint64(16)
	valueStride := uint64(32)

	if valueType == r.profile.Symbols["$sSSN"] {
		if keyType != valueType {
			panic("unsupported typed dictionary bridge")
		}

		typeRef, valueStride = 0x1a2e1d7c6, 16
	}

	if keyType == r.profile.Symbols["$ss11AnyHashableVN"] {
		typeRef, keyStride = 0x1a2e1db1a, 40
	}

	metadata := r.profile.SwiftTypes[strconv.FormatUint(typeRef, 10)]
	if metadata == 0 {
		panic("String/Any dictionary metadata not prepared")
	}

	slot := r.alloc(8)
	r.write64(slot, empty)
	r.ensureUniqueDictionary(slot, false, uint64(len(values)), metadata)
	storage := r.read64(slot)
	keys, items := r.read64(storage+48), r.read64(storage+56)
	seed := r.read64(storage + 40)
	buckets := uint64(1) << uint8(r.read64(storage+32))

	names := make([]string, 0, len(values))
	for key := range values {
		names = append(names, key)
	}

	sort.Strings(names)

	for _, key := range names {
		hasher := r.alloc(72)
		r.initHasher(hasher, seed)
		r.hashBytes(hasher, append([]byte(norm.NFC.String(key)), 255))
		hash := r.hashCompress(hasher, r.read64(hasher), true)

		index := hash & (buckets - 1)
		for r.read64(storage+64+(index/64)*8)&(1<<(index%64)) != 0 {
			index = (index + 1) & (buckets - 1)
		}

		bitmap := storage + 64 + (index/64)*8
		r.write64(bitmap, r.read64(bitmap)|(1<<(index%64)))
		r.returnSwiftString(key)
		r.write64(keys+index*keyStride, r.arg(0))
		r.write64(keys+index*keyStride+8, r.arg(1))

		if keyStride == 40 {
			r.write64(keys+index*keyStride+24, r.profile.Bridges["anyHashableStringMetadata"])
			r.write64(keys+index*keyStride+32, r.profile.Bridges["anyHashableStringWitness"])
		}

		if valueStride == 16 {
			value, ok := r.objects[values[key]].Value.(string)
			if !ok {
				panic("non-string value in typed dictionary bridge")
			}

			r.returnSwiftString(value)
			r.write64(items+index*16, r.arg(0))
			r.write64(items+index*16+8, r.arg(1))
		} else {
			r.storeSwiftAny(items+index*32, values[key])
		}
	}

	r.write64(storage+16, uint64(len(values)))

	return storage
}
func (r *Runtime) storeSwiftAny(destination, object uint64) {
	value := r.objects[object]
	if value == nil {
		panic("unknown object bridged to Swift Any")
	}

	var metadata uint64

	switch v := value.Value.(type) {
	case string:
		r.returnSwiftString(v)
		r.write64(destination, r.arg(0))
		r.write64(destination+8, r.arg(1))
		metadata = r.profile.Symbols["$sSSN"]
	case int64:
		r.write64(destination, uint64(v))
		metadata = r.profile.Symbols["$sSiN"]
	case bool:
		r.write64(destination, boolValue(v))
		metadata = r.profile.Symbols["$sSbN"]
	case float64:
		r.write64(destination, math.Float64bits(v))
		metadata = r.profile.Symbols["$sSdN"]
	default:
		class := value.Class
		if strings.Contains(class, "Dictionary") {
			class = "NSDictionary"
		} else if strings.Contains(class, "Array") {
			class = "NSArray"
		} else if strings.Contains(class, "Data") {
			class = "NSData"
		}

		metadata = r.profile.ObjCMetadata[class]
		r.write64(destination, object)
	}

	if metadata == 0 {
		panic("unprepared Swift Any metadata")
	}

	r.write64(destination+24, metadata)
}

func (r *Runtime) bridgeDictionaryToObjC(storage, keyType, valueType uint64) uint64 {
	result := map[string]uint64{}
	count := r.read64(storage + 16)

	if count == 0 {
		return r.New("NSDictionary", result)
	}

	if keyType != r.profile.Symbols["$sSSN"] || valueType != r.profile.Symbols["$sSSN"] {
		panic(fmt.Sprintf("unsupported dictionary Objective-C bridge types %#x/%#x", keyType, valueType))
	}

	if count > 768 || r.read64(storage+32)&255 > 10 {
		panic("dictionary bridge exceeds budget")
	}

	keys, values := r.read64(storage+48), r.read64(storage+56)

	buckets := uint64(1) << uint8(r.read64(storage+32))
	for index := uint64(0); index < buckets; index++ {
		if r.read64(storage+64+(index/64)*8)&(1<<(index%64)) == 0 {
			continue
		}

		key := r.swiftString(r.read64(keys+index*16), r.read64(keys+index*16+8))
		value := r.swiftString(r.read64(values+index*16), r.read64(values+index*16+8))
		result[key] = r.New("NSString", value)
	}

	if uint64(len(result)) != count {
		panic("Swift dictionary count mismatch")
	}

	return r.New("NSDictionary", result)
}
