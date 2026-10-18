package unicorn

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Demand-loaded guest memory", func() {
	It("retries an unmapped instruction fetch and releases its callback", func() {
		engine, err := New(context.Background())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(engine.Close)
		const address = uint64(0x6000000)
		count := 0
		var callbackErr error
		hook, err := engine.AddMemoryFaultHook(func(a uint64, _ uint32) bool {
			count++
			if a != address {
				return false
			}
			callbackErr = engine.MemMap(address, 4096)
			if callbackErr != nil {
				return false
			}
			callbackErr = engine.MemWrite(address, []byte{0xe0, 0, 0x80, 0xd2})

			return callbackErr == nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(engine.Start(address, address+4)).To(Succeed())
		Expect(callbackErr).NotTo(HaveOccurred())
		Expect(count).To(Equal(1))
		value, err := engine.RegRead(RegX0)
		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal(uint64(7)))
		id := hook.id
		Expect(hook.Close()).To(Succeed())
		_, present := codeHookCallbacks.Load(id)
		Expect(present).To(BeFalse())
	})
	It("propagates an unresolved memory fault", func() {
		engine, err := New(context.Background())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(engine.Close)
		_, err = engine.AddMemoryFaultHook(func(uint64, uint32) bool { return false })
		Expect(err).NotTo(HaveOccurred())
		Expect(engine.Start(0x6000000, 0x6000004)).NotTo(Succeed())
	})
})
