package machine

import (
	"context"
	"encoding/binary"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("StoreAgent kbsync", func() {
	It("pins the kbsync entry in the verified StoreAgent image", func() {
		Expect(storeAgentKBSyncEntry).To(Equal(storeAgentBase + 0x0c93c0))
	})

	It("rejects a canceled context before loading assets", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := GenerateKBSync(ctx, assets.Bundle{}, []byte{1}, 123)
		Expect(err).To(MatchError(ContainSubstring(context.Canceled.Error())))
	})

	It("rejects missing context or account identity before loading assets", func() {
		var missingContext context.Context
		_, err := GenerateKBSync(missingContext, assets.Bundle{}, nil, 123)
		Expect(err).To(MatchError("StoreAgent context is nil"))
		_, err = GenerateKBSync(context.Background(), assets.Bundle{}, nil, 0)
		Expect(err).To(MatchError("kbsync requires a nonzero account DSID"))
	})

	DescribeTable("copies and disposes guest output with the StoreAgent ABI",
		func(pointer, length uint64, status uint32, wantError string) {
			guest := newGinkgoServiceMachine(shimOptions{})
			data := []byte{0, 4, 0, 1, 42}
			Expect(guest.engine.MemWrite(heapBase, data)).To(Succeed())
			var disposed []uint64

			dispose, err := guest.services.addFunction("test.kbsync.dispose", func() error {
				address, err := guest.services.argument(0)
				Expect(err).NotTo(HaveOccurred())
				disposed = append(disposed, address)

				return guest.services.setResult(0)
			})
			Expect(err).NotTo(HaveOccurred())
			guest.entry.dispose = dispose

			entry, err := guest.services.addFunction("test.kbsync.generate", func() error {
				for index, expected := range []uint64{0x1234, 0x123456789abcdef0, 0, 1} {
					argument, err := guest.services.argument(index)
					Expect(err).NotTo(HaveOccurred())
					Expect(argument).To(Equal(expected))
				}

				pointerField, err := guest.services.argument(4)
				Expect(err).NotTo(HaveOccurred())
				Expect(guest.writeUint64(pointerField, pointer)).To(Succeed())
				lengthField, err := guest.services.argument(5)
				Expect(err).NotTo(HaveOccurred())
				// Only the low 32 bits are written by the real entry point.
				Expect(guest.engine.MemWrite(lengthField, binary.LittleEndian.AppendUint32(nil, uint32(length)))).To(Succeed())

				return guest.services.setResult(uint64(status))
			})
			Expect(err).NotTo(HaveOccurred())

			output, err := guest.generateKBSync(entry, 0x1234, 0x123456789abcdef0)
			if wantError == "" {
				Expect(err).NotTo(HaveOccurred())
				Expect(output).To(Equal(data))
			} else {
				Expect(err).To(MatchError(ContainSubstring(wantError)))
				Expect(output).To(BeNil())
			}

			if pointer != 0 {
				Expect(disposed).To(Equal([]uint64{pointer}))
			} else {
				Expect(disposed).To(BeEmpty())
			}
			Expect(guest.scratchCursor).To(BeZero())
			Expect(guest.engine.MemRead(scratchBase, 32)).To(Equal(make([]byte, 32)))
		},
		Entry("valid blob", heapBase, uint64(5), uint32(0), ""),
		Entry("guest error still releases storage", heapBase, uint64(5), ^uint32(41), "kbsync returned -42"),
		Entry("oversized blob", heapBase, maxOutputSize+1, uint32(0), "maximum is"),
		Entry("null pointer", uint64(0), uint64(5), uint32(0), "null output pointer"),
		Entry("empty blob", heapBase, uint64(0), uint32(0), "empty buffer"),
	)
})
