package machine

import (
	"math"

	"github.com/majd/ipatool/v2/internal/sap/unicorn"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("guest once services", func() {
	It("runs a pthread initializer once with a valid call frame", func() {
		machine := newGinkgoServiceMachine(shimOptions{})
		control := scratchBase
		Expect(machine.services.writeUint64(control, 0x30b1bcba)).To(Succeed())

		calls := 0
		initializer, err := machine.services.addFunction("test.pthread.initializer", func() error {
			stack, readErr := machine.engine.RegRead(unicorn.RegSP)
			Expect(readErr).NotTo(HaveOccurred())
			Expect(stack % 16).To(BeZero())
			calls++

			return machine.services.setResult(0xdeadbeef)
		})
		Expect(err).NotTo(HaveOccurred())

		for range 2 {
			result, invokeErr := machine.invoke(machine.services.symbols["_pthread_once"], control, initializer)
			Expect(invokeErr).NotTo(HaveOccurred())
			Expect(result).To(BeZero())
		}

		Expect(calls).To(Equal(1))
		Expect(machine.services.readUint64(control)).To(Equal(uint64(0x4f4e4345)))
	})

	It("runs a dispatch block once", func() {
		machine := newGinkgoServiceMachine(shimOptions{})
		predicate := scratchBase
		block := scratchBase + 0x100
		calls := 0

		initializer, err := machine.services.addFunction("test.dispatch.initializer", func() error {
			argument, argumentErr := machine.services.argument(0)
			Expect(argumentErr).NotTo(HaveOccurred())
			Expect(argument).To(Equal(block))
			calls++

			return machine.services.setResult(0xdeadbeef)
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(machine.services.writeUint64(block+16, initializer)).To(Succeed())

		for range 2 {
			result, invokeErr := machine.invoke(machine.services.symbols["_dispatch_once"], predicate, block)
			Expect(invokeErr).NotTo(HaveOccurred())
			Expect(result).To(BeZero())
		}

		Expect(calls).To(Equal(1))
		Expect(machine.services.readUint64(predicate)).To(Equal(uint64(math.MaxUint64)))
	})
})
