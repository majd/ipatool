package profilegen

import (
	"encoding/binary"
	"fmt"

	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
)

func (v *bootstrapVM) realizeStoreClasses(classes []profileformat.Class) error {
	for i := range classes {
		c := &classes[i]
		if len(c.Methods) == 0 || v.u64(c.Address+32)&2 == 0 {
			continue
		}

		descriptor := v.u64(c.Address + 64)
		if descriptor == 0 {
			continue
		}

		flags := binary.LittleEndian.Uint32(v.read(descriptor, 4))
		if flags&0x80 != 0 {
			continue
		}

		relative := int32(binary.LittleEndian.Uint32(v.read(descriptor+12, 4)))
		if relative == 0 {
			continue
		}

		accessor := uint64(int64(descriptor+12) + int64(relative))

		address, state, err := v.callAddress(accessor, 0)
		if err != nil {
			return fmt.Errorf("realize %s: %w", c.Name, err)
		}

		if address != c.Address || state != 0 {
			return fmt.Errorf("unexpected class metadata for %s: %#x state %d", c.Name, address, state)
		}

		c.Size = max(c.Size, (uint64(binary.LittleEndian.Uint32(v.read(c.Address+48, 4)))+7)&^7)
		ro := v.u64(c.Address+32) &^ 7

		list := v.u64(ro + 48)
		if list != 0 {
			count := binary.LittleEndian.Uint32(v.read(list+4, 4))
			stride := binary.LittleEndian.Uint32(v.read(list, 4))

			for j := uint32(0); j < count; j++ {
				entry := list + 8 + uint64(j)*uint64(stride)
				name := v.string(v.u64(entry + 8))

				offset := v.u64(entry)
				if offset != 0 {
					c.Ivars[name] = uint64(binary.LittleEndian.Uint32(v.read(offset, 4)))
				}
			}
		}
	}

	return nil
}
