package machimage

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/blacktop/go-macho/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// One instruction page and one data page following the Mach-O headers.
func sharedFixture() []byte {
	data := make([]byte, 3*pageSize)
	for n, v := range []uint32{0xfeedfacf, uint32(types.CPUArm64), uint32(types.CPUSubtypeArm64E), 6, 2, 176, 0, 0} {
		binary.LittleEndian.PutUint32(data[n*4:], v)
	}

	binary.LittleEndian.PutUint32(data[32:], 25)
	binary.LittleEndian.PutUint32(data[36:], 152)
	copy(data[40:], "__TEXT")

	for n, v := range []uint64{0x100000000, uint64(len(data)), 0, uint64(len(data))} {
		binary.LittleEndian.PutUint64(data[56+n*8:], v)
	}

	for n, v := range []uint32{7, 7, 1, 0} {
		binary.LittleEndian.PutUint32(data[88+n*4:], v)
	}

	copy(data[104:], "__text")
	copy(data[120:], "__TEXT")
	binary.LittleEndian.PutUint64(data[136:], 0x100001000)
	binary.LittleEndian.PutUint64(data[144:], pageSize)
	binary.LittleEndian.PutUint32(data[152:], uint32(pageSize))
	binary.LittleEndian.PutUint32(data[156:], 2)
	binary.LittleEndian.PutUint32(data[168:], 0x80000400)

	for n, v := range []uint32{2, 24, 512, 1, 528, 8} {
		binary.LittleEndian.PutUint32(data[184+n*4:], v)
	}

	binary.LittleEndian.PutUint32(data[512:], 1)
	data[516], data[517] = 0x0f, 1
	binary.LittleEndian.PutUint64(data[520:], 0x100001000)
	copy(data[528:], []byte{0, 't', 'a', 'r', 'g', 'e', 't', 0})
	binary.LittleEndian.PutUint32(data[4096:], 0xd503233f)

	return data
}

type imageMemory struct {
	data []byte
	base uint64
	fail error
}

func (m *imageMemory) MemMap(uint64, uint64) error { return nil }
func (m *imageMemory) MemWrite(address uint64, data []byte) error {
	if m.fail != nil {
		return m.fail
	}

	copy(m.data[address-m.base:], data)

	return nil
}

var _ = Describe("Shared image preparation", func() {
	It("shares instruction patches and isolates relocation and source bytes", func() {
		source := sharedFixture()
		original := bytes.Clone(source)
		first, err := OpenShared("first", source)
		Expect(err).NotTo(HaveOccurred())
		second, err := OpenShared("second", source)
		Expect(err).NotTo(HaveOccurred())
		Expect(&first.data[0]).To(BeIdenticalTo(&source[0]))
		Expect(&first.instructions[4096][0]).To(BeIdenticalTo(&second.instructions[4096][0]))
		Expect(first.modified).To(BeEmpty())
		Expect(first.file.Symtab).To(BeIdenticalTo(second.file.Symtab))
		exported, err := second.Export("target", 0x300000000)
		Expect(err).NotTo(HaveOccurred())
		Expect(exported).To(Equal(uint64(0x300001000)))
		Expect(second.InstructionRanges()).To(Equal([][2]uint64{{0x100001000, 0x100002000}}))
		Expect(second.Symbols()).To(HaveLen(1))
		first.binds = []types.Bind{{Name: "target", Segment: "__TEXT", SegOffset: 8192, Type: types.BIND_TYPE_POINTER}}
		second.binds = first.binds
		Expect(first.Relocate(0x200000000, func(string) (uint64, error) { return 0x1111, nil })).To(Succeed())
		Expect(second.Relocate(0x300000000, func(string) (uint64, error) { return 0x2222, nil })).To(Succeed())
		Expect(second.putPointer(4092, 0x8877665544332211)).To(Succeed())
		Expect(second.pointerAt(4092)).To(Equal(uint64(0x8877665544332211)))
		Expect(first.pointerAt(4092)).To(Equal(uint64(0xd503201f00000000)))
		for _, test := range []struct {
			image       *Image
			base, value uint64
		}{{first, 0x200000000, 0x1111}, {second, 0x300000000, 0x2222}} {
			memory := &imageMemory{base: test.base, data: make([]byte, len(source))}
			Expect(test.image.Load(memory)).To(Succeed())
			Expect(binary.LittleEndian.Uint64(memory.data[8192:])).To(Equal(test.value))
			if test.image == first {
				Expect(binary.LittleEndian.Uint32(memory.data[4096:])).To(Equal(uint32(0xd503201f)))
			}
		}
		Expect(source).To(Equal(original))
	})
	It("keeps concurrent guests independent while sharing preparation", func() {
		source := sharedFixture()
		source[len(source)-2] = 77
		finished := make(chan error, 8)
		for n := range 8 {
			go func() {
				image, err := OpenShared("concurrent", source)
				if err == nil {
					image.binds = []types.Bind{{Name: "target", Segment: "__TEXT", SegOffset: 8192, Type: types.BIND_TYPE_POINTER}}
					expected := uint64(n+1) * 4096
					err = image.Relocate(uint64(n+1)<<32, func(string) (uint64, error) { return expected, nil })
					if err == nil && image.pointerAt(8192) != expected {
						err = fmt.Errorf("guest %d has another guest's binding", n)
					}
				}
				finished <- err
			}()
		}
		for range 8 {
			Eventually(finished).Should(Receive(BeNil()))
		}
	})

	It("applies cached chained relocations with each guest's own addresses", func() {
		source := make([]byte, 4096)
		plan := []chainedRelocation{{offset: 0, target: 0x80}, {offset: 8, isBind: true, symbol: "target", addend: -4}}
		for _, base := range []uint64{0x1000, 0x2000} {
			image := &Image{data: source, modified: map[uint64][]byte{}, fixups: plan}
			Expect(image.relocateChained(base, func(string) (uint64, error) { return base + 0x404, nil })).To(Succeed())
			Expect(image.pointerAt(0)).To(Equal(base + 0x80))
			Expect(image.pointerAt(8)).To(Equal(base + 0x400))
		}
		Expect(source).To(Equal(make([]byte, 4096)))
	})
	It("overlays patches across unaligned page, segment, and chunk boundaries", func() {
		source := make([]byte, 3*pageSize+13)
		image := &Image{data: source, modified: map[uint64][]byte{}}
		image.writeInstruction(4094, 0x11223344)
		Expect(image.pointerAt(4092)).To(Equal(uint64(0x112233440000)))
		var decoded [8]byte
		count, err := (imageInput{image}).ReadAt(decoded[:], 4092)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(8))
		Expect(binary.LittleEndian.Uint64(decoded[:])).To(Equal(uint64(0x112233440000)))
		Expect(image.putPointer(4095, 0xaabbccddeeff0011)).To(Succeed())
		expected := bytes.Clone(source)
		binary.LittleEndian.PutUint32(expected[4094:], 0x11223344)
		binary.LittleEndian.PutUint64(expected[4095:], 0xaabbccddeeff0011)
		memory := &imageMemory{data: make([]byte, len(source))}
		Expect(image.writeSegment(memory, 0, 3, uint64(len(source)), make([]byte, 4093))).To(Succeed())
		Expect(memory.data[:len(source)-3]).To(Equal(expected[3:]))
	})
	It("invalidates preparation when any source byte changes", func() {
		source := sharedFixture()
		first, err := OpenShared("first", source)
		Expect(err).NotTo(HaveOccurred())
		changed := bytes.Clone(source)
		changed[len(changed)-1] = 1
		second, err := OpenShared("second", changed)
		Expect(err).NotTo(HaveOccurred())
		Expect(&first.instructions[4096][0]).NotTo(BeIdenticalTo(&second.instructions[4096][0]))
	})
	It("preserves write errors and rejects fixups outside the image", func() {
		image, err := OpenShared("image", sharedFixture())
		Expect(err).NotTo(HaveOccurred())
		Expect(image.Relocate(image.Base(), nil)).To(Succeed())
		failed := errors.New("write failure")
		Expect(errors.Is(image.Load(&imageMemory{fail: failed}), failed)).To(BeTrue())
		Expect(image.putPointer(uint64(len(image.data)-7), 1)).NotTo(Succeed())
	})
})

var _ = Describe("Image preparation cache bounds", func() {
	It("evicts old preparations when the image count is exceeded", func() {
		var cache imagePreparationCache
		first := sha256.Sum256([]byte{0})
		for n := 0; n <= maxImagePreparations; n++ {
			cache.put(sha256.Sum256([]byte{byte(n)}), imagePreparation{})
		}
		_, ok := cache.get(first)
		Expect(ok).To(BeFalse())
		Expect(cache.entries).To(HaveLen(maxImagePreparations))
	})
	It("bounds retained patch and fixup storage", func() {
		var cache imagePreparationCache
		key := sha256.Sum256([]byte("first"))
		large := imagePreparation{instructions: map[uint64][]instructionPatch{0: make([]instructionPatch, 0, maxPreparationBytes/16+1)}}
		cache.put(key, large)
		_, ok := cache.get(key)
		Expect(ok).To(BeFalse())
		half := imagePreparation{instructions: map[uint64][]instructionPatch{0: large.instructions[0][: 0 : maxPreparationBytes/32+1]}}
		cache.put(key, half)
		second := sha256.Sum256([]byte("second"))
		cache.put(second, half)
		_, ok = cache.get(key)
		Expect(ok).To(BeFalse())
		Expect(cache.bytes).To(BeNumerically("<=", maxPreparationBytes))
	})
})
