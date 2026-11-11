package machimage

import (
	"crypto/sha256"
	"sync"
	"unsafe"

	"github.com/blacktop/go-macho"
	"github.com/blacktop/go-macho/types"
)

const (
	maxImagePreparations = 8
	maxPreparationBytes  = 64 << 20
)

type chainedRelocation struct {
	offset, target uint64
	addend         int64
	symbol         string
	isBind         bool
}

type instructionPatch struct {
	offset uint64
	value  uint32
}

type imagePreparation struct {
	instructions map[uint64][]instructionPatch
	fixups       []chainedRelocation
	binds        []types.Bind
	rebases      []types.Rebase
	symtab       *macho.Symtab
	dysymtab     *macho.Dysymtab
}

func (p imagePreparation) size() int {
	size := cap(p.fixups)*int(unsafe.Sizeof(chainedRelocation{})) + cap(p.binds)*int(unsafe.Sizeof(types.Bind{})) + cap(p.rebases)*int(unsafe.Sizeof(types.Rebase{}))
	for _, patches := range p.instructions {
		size += cap(patches) * int(unsafe.Sizeof(instructionPatch{}))
	}

	for _, fixup := range p.fixups {
		size += len(fixup.symbol)
	}

	for _, bind := range p.binds {
		size += len(bind.Name)
	}

	if p.symtab != nil {
		size += cap(p.symtab.Syms)*int(unsafe.Sizeof(macho.Symbol{})) + cap(p.symtab.LoadBytes)
		for _, symbol := range p.symtab.Syms {
			size += len(symbol.Name) + len(symbol.IndirectName)
		}
	}

	if p.dysymtab != nil {
		size += cap(p.dysymtab.IndirectSyms)*4 + cap(p.dysymtab.LoadBytes)
	}

	return size
}

type imagePreparationCache struct {
	mu      sync.Mutex
	entries map[[sha256.Size]byte]imagePreparation
	order   [][sha256.Size]byte
	bytes   int
}

// Cache only immutable instruction patches, symbol tables and decoded relocations. Every guest
// supplies its own load bases, import addresses and mutable memory.
var imagePreparations imagePreparationCache

func (c *imagePreparationCache) get(key [sha256.Size]byte) (imagePreparation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prepared, ok := c.entries[key]

	return prepared, ok
}

func (c *imagePreparationCache) put(key [sha256.Size]byte, prepared imagePreparation) {
	size := prepared.size()
	if size > maxPreparationBytes {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.entries[key]; exists {
		return
	}

	if c.entries == nil {
		c.entries = make(map[[sha256.Size]byte]imagePreparation)
	}

	for len(c.order) >= maxImagePreparations || c.bytes+size > maxPreparationBytes {
		oldest := c.order[0]
		c.bytes -= c.entries[oldest].size()
		delete(c.entries, oldest)
		c.order = c.order[1:]
	}

	c.order = append(c.order, key)
	c.entries[key] = prepared
	c.bytes += size
}
