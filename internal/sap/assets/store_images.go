package assets

import (
	"encoding/binary"
	"fmt"

	"github.com/blacktop/go-macho"
	machotypes "github.com/blacktop/go-macho/types"
)

// AddressRange reports the unslid virtual range occupied by cache mappings.
func (s *StoreCache) AddressRange() (uint64, uint64) {
	start, end := ^uint64(0), uint64(0)

	for _, member := range s.cache.members {
		for _, mapping := range member.mappings {
			start = min(start, mapping.address)
			end = max(end, mapping.address+mapping.size)
		}
	}

	return start, end - start
}

// Images lists the image install names recorded in the downloaded shared cache.
func (s *StoreCache) Images() (map[string]uint64, error) {
	if s.images != nil {
		return s.images, nil
	}

	header := make([]byte, dyldHeaderSize)
	if _, err := s.cache.primary.file.ReadAt(header, 0); err != nil {
		return nil, fmt.Errorf("read Store cache header: %w", err)
	}

	offset := uint64(binary.LittleEndian.Uint32(header[448:]))
	count := uint64(binary.LittleEndian.Uint32(header[452:]))

	if count == 0 || count > dyldMaximumImages || !validDyldRange(s.cache.primary.size, offset, count*dyldImageInfoSize) {
		return nil, fmt.Errorf("invalid Store image table")
	}

	data := make([]byte, count*dyldImageInfoSize)
	if _, err := s.cache.primary.file.ReadAt(data, int64(offset)); err != nil {
		return nil, fmt.Errorf("read Store image table: %w", err)
	}

	images := make(map[string]uint64, count)

	for index := uint64(0); index < count; index++ {
		entry := data[index*dyldImageInfoSize:]

		name, err := readDyldCString(s.cache.primary.file, s.cache.primary.size, uint64(binary.LittleEndian.Uint32(entry[24:])))
		if err != nil {
			return nil, err
		}

		images[name] = binary.LittleEndian.Uint64(entry)
	}

	s.images = images

	return images, nil
}

// OpenImage exposes Mach-O metadata directly from the cache; no host image is loaded.
func (s *StoreCache) OpenImage(name string) (*macho.File, error) {
	if s.cache.relativeSelectorBase == 0 {
		objc, err := s.cache.image("/usr/lib/libobjc.A.dylib")
		if err != nil {
			return nil, err
		}

		f, err := s.cache.openImage(objc, machotypes.LC_SYMTAB, machotypes.LC_DYSYMTAB)
		if err != nil {
			return nil, err
		}

		defer f.Close()

		for _, section := range f.Sections {
			if section.Name == "__objc_opt_ro" {
				page, err := s.Page(section.Addr &^ 4095)
				if err != nil {
					return nil, err
				}

				off := section.Addr & 4095
				if off+48 > uint64(len(page)) || section.Size < 48 {
					return nil, fmt.Errorf("invalid ObjC optimization header")
				}

				if binary.LittleEndian.Uint32(page[off:]) != 16 {
					return nil, fmt.Errorf("unsupported ObjC optimization version")
				}

				s.cache.relativeSelectorBase = section.Addr + binary.LittleEndian.Uint64(page[off+40:])
			}
		}
	}

	images, err := s.Images()
	if err != nil {
		return nil, err
	}

	address, ok := images[name]
	if !ok {
		return nil, fmt.Errorf("image %s was not found", name)
	}

	mapping, err := s.cache.mappingForAddress(address)
	if err != nil {
		return nil, err
	}

	return s.cache.openImage(&dyldImage{member: mapping.member, address: address, offset: mapping.fileOffset + address - mapping.address}, machotypes.LC_SYMTAB, machotypes.LC_DYSYMTAB)
}

// Contains reports whether a range is backed by one shared-cache mapping.
func (s *StoreCache) Contains(address, size uint64) bool {
	for _, member := range s.cache.members {
		for _, m := range member.mappings {
			if address >= m.address && address-m.address <= m.size && size <= m.size-(address-m.address) {
				return true
			}
		}
	}

	return false
}
