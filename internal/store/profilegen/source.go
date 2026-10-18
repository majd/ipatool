// Package profilegen bootstraps Store metadata from pinned, on-disk Apple images.
package profilegen

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/blacktop/go-macho"
	"github.com/majd/ipatool/v2/internal/sap/assets"
	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
)

const amsImage = "/System/Library/PrivateFrameworks/AppleMediaServices.framework/Versions/A/AppleMediaServices"
const authImage = "/System/Library/Accounts/Authentication/AMSAccountAuthenticationPlugin.bundle/Contents/MacOS/AMSAccountAuthenticationPlugin"

type source struct {
	ctx           context.Context
	cache         *assets.StoreCache
	pages         map[uint64][]byte
	images        map[string]*macho.File
	exports       map[string]uint64
	symbols       map[uint64]string
	symbolsLoaded map[string]bool
	code          [][2]uint64
	target        [][2]uint64
	selectorBase  uint64
	classes       map[uint64]*profileformat.Class
}

func newSource(ctx context.Context, cache *assets.StoreCache) (*source, error) {
	s := &source{ctx: ctx, cache: cache, pages: map[uint64][]byte{}, images: map[string]*macho.File{}, exports: map[string]uint64{}, symbols: map[uint64]string{}, symbolsLoaded: map[string]bool{}, classes: map[uint64]*profileformat.Class{}}
	if e := s.loadImage(amsImage); e != nil {
		s.close()

		return nil, fmt.Errorf("load bootstrap metadata: %w", e)
	}

	if e := s.loadImage(authImage); e != nil {
		s.close()

		return nil, fmt.Errorf("load bootstrap metadata: %w", e)
	}

	if s.images[amsImage].UUID().String() != FrameworkUUID || s.images[authImage].UUID().String() != PluginUUID {
		s.close()

		return nil, fmt.Errorf("store images do not match pinned macOS 26A434")
	}

	objc := s.images["/usr/lib/libobjc.A.dylib"]
	if objc == nil {
		return nil, fmt.Errorf("libobjc is not a Store dependency")
	}

	for _, sec := range objc.Sections {
		if sec.Name == "__objc_opt_ro" {
			s.selectorBase = sec.Addr + s.u64(sec.Addr+40)
		}
	}

	sort.Slice(s.code, func(i, j int) bool { return s.code[i][0] < s.code[j][0] })
	sort.Slice(s.target, func(i, j int) bool { return s.target[i][0] < s.target[j][0] })

	return s, nil
}
func (s *source) close() {
	for _, f := range s.images {
		_ = f.Close()
	}
}
func (s *source) loadImage(name string) error {
	if _, ok := s.images[name]; ok {
		return nil
	}

	if e := s.ctx.Err(); e != nil {
		return fmt.Errorf("load bootstrap metadata: %w", e)
	}

	f, e := s.cache.OpenImage(name)
	if e != nil {
		return fmt.Errorf("load %s: %w", name, e)
	}

	s.images[name] = f

	exports, e := f.DyldExports()
	if e != nil && f.DyldExportsTrie() != nil {
		return fmt.Errorf("load bootstrap metadata: %w", e)
	}

	for _, v := range exports {
		if v.Address != 0 && !v.Flags.ReExport() {
			s.exports[v.Name] = v.Address
			s.symbols[v.Address] = strings.TrimPrefix(v.Name, "_")
		}
	}

	for _, sec := range f.Sections {
		if sec.Flags.IsPureInstructions() || sec.Flags.IsSomeInstructions() {
			s.code = append(s.code, [2]uint64{sec.Addr, sec.Addr + sec.Size})
		}
	}

	if name == amsImage || name == authImage {
		for _, seg := range f.Segments() {
			if seg.Name != "__LINKEDIT" {
				s.target = append(s.target, [2]uint64{seg.Addr, seg.Addr + seg.Memsz})
			}
		}
	}

	weak := map[string]bool{}
	delayed := map[string]bool{}

	for _, load := range f.Loads {
		switch d := load.(type) {
		case *macho.WeakDylib:
			weak[d.Name] = true
		case *macho.LoadDylib:
			if uint32(d.Flags)&8 != 0 {
				delayed[d.Name] = true
			}

			if uint32(d.Flags)&1 != 0 {
				weak[d.Name] = true
			}
		}
	}

	for _, dep := range f.ImportedLibraries() {
		if delayed[dep] {
			continue
		}

		if weak[dep] {
			images, err := s.cache.Images()
			if err != nil {
				return fmt.Errorf("load bootstrap metadata: %w", err)
			}

			if _, ok := images[dep]; !ok {
				continue
			}
		}

		if e = s.loadImage(dep); e != nil {
			return fmt.Errorf("dependency of %s: %w", name, e)
		}
	}

	return nil
}
func (s *source) read(address, size uint64) []byte {
	if size > 64<<20 {
		panic("bootstrap read exceeds size limit")
	}

	data := make([]byte, size)

	for offset := uint64(0); offset < size; {
		addr := address + offset
		base := addr &^ 4095

		p, ok := s.pages[base]
		if !ok {
			var e error

			p, e = s.cache.Page(base)
			if e != nil {
				panic(e)
			}

			s.pages[base] = p
		}

		n := min(size-offset, 4096-(addr&4095))
		copy(data[offset:offset+n], p[addr&4095:(addr&4095)+n])
		offset += n
	}

	return data
}
func (s *source) u64(a uint64) uint64 { return binary.LittleEndian.Uint64(s.read(a, 8)) }
func (s *source) u32(a uint64) uint32 { return binary.LittleEndian.Uint32(s.read(a, 4)) }
func (s *source) cstring(a uint64) string {
	if a == 0 {
		return ""
	}

	var out []byte
	for len(out) < 1<<20 {
		n := uint64(4096) - (a & 4095)

		p := s.read(a, n)
		if i := bytes.IndexByte(p, 0); i >= 0 {
			return string(append(out, p[:i]...))
		}

		out = append(out, p...)
		a += n
	}

	panic("unterminated bootstrap string")
}
func inside(a uint64, regions [][2]uint64) bool {
	i := sort.Search(len(regions), func(i int) bool { return regions[i][0] > a }) - 1

	return i >= 0 && a < regions[i][1]
}
func (s *source) imageNames() []string {
	names := make([]string, 0, len(s.images))
	for name := range s.images {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

func (s *source) describe(a uint64) string {
	var best uint64

	var name string

	for address, n := range s.symbols {
		if address <= a && address > best {
			best = address
			name = n
		}
	}

	return fmt.Sprintf("%s+%#x", name, a-best)
}
