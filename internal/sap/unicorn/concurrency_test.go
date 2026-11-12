package unicorn

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Concurrent Unicorn engines", func() {
	It("does not time out active engines while initializing other engines", func() {
		const address = uint64(0x1200000)
		const count = 8

		engines := make([]*Engine, count)
		started := make(chan struct{}, count)
		results := make(chan error, count)

		for i := range engines {
			engine, err := New(context.Background())
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(engine.Close)
			engines[i] = engine
			Expect(engine.MemMap(address, 4096)).To(Succeed())
			Expect(engine.MemWrite(address, []byte{0, 0, 0, 0x14})).To(Succeed()) // b .

			var deadline time.Time

			_, err = engine.AddCodeHook(address, address, func(uint64, uint32) {
				if deadline.IsZero() {
					deadline = time.Now().Add(time.Second)
					started <- struct{}{}
				}

				time.Sleep(100 * time.Microsecond)

				if time.Now().After(deadline) {
					_ = engine.Stop()
				}
			})
			Expect(err).NotTo(HaveOccurred())
		}

		for _, engine := range engines {
			go func() {
				results <- engine.StartBounded(address, address+4, time.Minute, 0)
			}()
		}

		for range count {
			Eventually(started).Should(Receive())
		}

		for range 200 {
			other, err := New(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(other.MemMap(address, 4096)).To(Succeed())
			Expect(other.Close()).To(Succeed())
		}

		for range count {
			var err error

			Eventually(results).Should(Receive(&err))
			Expect(err).NotTo(HaveOccurred())
		}
	})
})
