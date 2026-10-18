package unicorn

import (
	"context"
	"encoding/binary"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Guarded grouped memory", func() {
	const base = uint64(0x6000000)
	var engine *Engine
	var missing error
	BeforeEach(func() {
		var err error
		engine, err = New(context.Background())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(engine.Close)
		Expect(engine.MemMap(base, 3*4096)).To(Succeed())
		missing = errors.New("missing dependency")
	})
	install := func() {
		Expect(engine.GuardMemory(func(address, size uint64) error {
			if size > 0 && address < base+8192 && address+size > base+4096 {
				return missing
			}

			return nil
		})).To(Succeed())
	}
	instruction := func(address uint64, word uint32) {
		var data [4]byte
		binary.LittleEndian.PutUint32(data[:], word)
		Expect(engine.MemWrite(address, data[:])).To(Succeed())
	}

	It("checks host reads, buffered reads and writes across a missing page", func() {
		install()
		_, err := engine.MemRead(base+4092, 8)
		Expect(errors.Is(err, missing)).To(BeTrue())
		Expect(errors.Is(engine.MemReadInto(make([]byte, 8), base+4092), missing)).To(BeTrue())
		Expect(errors.Is(engine.MemWrite(base+4092, make([]byte, 8)), missing)).To(BeTrue())
		Expect(engine.MemWrite(base, []byte{1})).To(Succeed())
		data, err := engine.MemRead(base, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(Equal([]byte{1}))
	})
	DescribeTable("reports guest data violations for both execution APIs", func(word uint32, bounded bool) {
		instruction(base, word)
		Expect(engine.RegWrite(RegX1, base+4096)).To(Succeed())
		install()
		var err error
		if bounded {
			err = engine.StartBounded(base, base+4, time.Second, 10)
		} else {
			err = engine.Start(base, base+4)
		}

		Expect(errors.Is(err, missing)).To(BeTrue())
		// A caller cannot accidentally continue after an ignored guard failure.
		Expect(errors.Is(engine.Start(base, base+4), missing)).To(BeTrue())
	}, Entry("read", uint32(0xf9400020), false), Entry("write", uint32(0xf9000020), false), Entry("bounded read", uint32(0xf9400020), true), Entry("bounded write", uint32(0xf9000020), true))
	It("rejects instruction execution in mapped padding", func() {
		instruction(base+4096, 0xd28000e0) // mov x0, #7
		install()
		Expect(errors.Is(engine.Start(base+4096, base+4100), missing)).To(BeTrue())
		value, err := engine.RegRead(RegX0)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(BeZero())
	})
	DescribeTable("rejects guest accesses crossing from captured data into padding", func(word uint32) {
		instruction(base, word)
		Expect(engine.RegWrite(RegX1, base+4092)).To(Succeed())
		install()
		Expect(errors.Is(engine.Start(base, base+4), missing)).To(BeTrue())
	}, Entry("read", uint32(0xf9400020)), Entry("write", uint32(0xf9000020)))
	It("checks instruction fetches even when the translation was cached", func() {
		instruction(base, 0xd28000e0)
		allowed := true
		Expect(engine.GuardMemory(func(uint64, uint64) error {
			if !allowed {
				return missing
			}

			return nil
		})).To(Succeed())
		Expect(engine.Start(base, base+4)).To(Succeed())
		allowed = false
		Expect(engine.RegWrite(RegX0, 0)).To(Succeed())
		Expect(errors.Is(engine.Start(base, base+4), missing)).To(BeTrue())
		value, err := engine.RegRead(RegX0)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(BeZero())
	})
	It("allows guest reads and writes on captured pages", func() {
		instruction(base, 0xf9000020)   // str x0, [x1]
		instruction(base+4, 0xf9400022) // ldr x2, [x1]
		Expect(engine.RegWrite(RegX0, 42)).To(Succeed())
		Expect(engine.RegWrite(RegX1, base+8192)).To(Succeed())
		install()
		Expect(engine.Start(base, base+8)).To(Succeed())
		value, err := engine.RegRead(RegX0 + 2)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal(uint64(42)))
	})
	It("rejects invalid installation and releases both callbacks on close", func() {
		Expect(engine.GuardMemory(nil)).To(MatchError(ContainSubstring("validator is nil")))
		install()
		Expect(engine.GuardMemory(func(uint64, uint64) error { return nil })).To(MatchError(ContainSubstring("already installed")))
		ids := make([]uintptr, 0, len(engine.hooks.ids))
		for id := range engine.hooks.ids {
			ids = append(ids, id)
		}

		Expect(ids).To(HaveLen(2))
		Expect(engine.Close()).To(Succeed())
		for _, id := range ids {
			_, present := codeHookCallbacks.Load(id)
			Expect(present).To(BeFalse())
		}
	})
})
