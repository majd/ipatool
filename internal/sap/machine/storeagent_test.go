package machine

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/sap/unicorn"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("StoreAgent", func() {
	Describe("profile", func() {
		It("pins the verified image base and entry offsets", func() {
			Expect(storeAgentBase).To(Equal(uint64(0x00001000c0000000)))
			Expect(storeAgentGlobalInit).To(Equal(storeAgentBase + 0x0c5fc0))
			Expect(storeAgentSessionInit).To(Equal(storeAgentBase + 0x0debd0))
			Expect(storeAgentDecryptEntry).To(Equal(storeAgentBase + 0x0ee700))
			Expect(storeAgentSessionClose).To(Equal(storeAgentBase + 0x1212d0))
		})

		It("re-verifies the pinned image before opening the runtime", func() {
			_, err := openStoreAgent(context.Background(), assets.Bundle{}, []byte("wrong image"), []byte{1}, []byte{1})
			Expect(err).To(MatchError(ContainSubstring("verify Apple StoreAgent profile")))
			Expect(err).To(MatchError(ContainSubstring("has size")))
		})

		It("adds only the StoreAgent synchronization aliases", func() {
			machine := newGinkgoServiceMachine(shimOptions{zeroReturnAliases: storeAgentZeroReturnAliases})

			for _, name := range storeAgentZeroReturnAliases {
				address, ok := machine.services.symbols[name]
				Expect(ok).To(BeTrue(), name)
				Expect(machine.invoke(address)).To(Equal(uint64(0)))
			}
		})

		It("does not add StoreAgent aliases to the default SAP profile", func() {
			machine := newGinkgoServiceMachine(shimOptions{})
			address, err := machine.services.resolve("_pthread_mutex_init")
			Expect(err).NotTo(HaveOccurred())
			_, err = machine.invoke(address)
			Expect(err).To(MatchError(ContainSubstring("unsupported import _pthread_mutex_init")))
		})
	})

	Describe("streaming decryption", func() {
		It("uses full 0x8000 chunks and a final partial chunk", func() {
			input := bytes.Repeat([]byte{0x3c}, 2*storeAgentChunkSize+17)
			var output bytes.Buffer
			var sizes []int

			written, err := decryptStream(context.Background(), &output, bytes.NewReader(input), func(chunk []byte) error {
				sizes = append(sizes, len(chunk))
				for index := range chunk {
					chunk[index] ^= 0xff
				}

				return nil
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(written).To(Equal(int64(len(input))))
			Expect(sizes).To(Equal([]int{storeAgentChunkSize, storeAgentChunkSize, 17}))
			Expect(output.Bytes()).To(Equal(bytes.Repeat([]byte{0xc3}, len(input))))
		})

		It("does not invoke the decryptor for an empty stream", func() {
			called := false
			written, err := decryptStream(context.Background(), io.Discard, bytes.NewReader(nil), func([]byte) error {
				called = true

				return nil
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(written).To(BeZero())
			Expect(called).To(BeFalse())
		})

		It("writes a partial read before returning its source error", func() {
			sourceErr := errors.New("source failed")
			source := io.MultiReader(bytes.NewReader([]byte("partial")), errorReader{err: sourceErr})
			var output bytes.Buffer

			written, err := decryptStream(context.Background(), &output, source, func(chunk []byte) error {
				return nil
			})

			Expect(written).To(Equal(int64(len("partial"))))
			Expect(output.String()).To(Equal("partial"))
			Expect(err).To(MatchError(ContainSubstring(sourceErr.Error())))
		})

		It("stops before the next chunk when the context is canceled", func() {
			ctx, cancel := context.WithCancel(context.Background())
			input := bytes.Repeat([]byte{1}, 2*storeAgentChunkSize)
			calls := 0

			written, err := decryptStream(ctx, io.Discard, bytes.NewReader(input), func([]byte) error {
				calls++
				cancel()

				return nil
			})

			Expect(calls).To(Equal(1))
			Expect(written).To(Equal(int64(storeAgentChunkSize)))
			Expect(err).To(MatchError(ContainSubstring(context.Canceled.Error())))
		})

		It("returns decryptor errors without writing the failed chunk", func() {
			decryptErr := errors.New("decrypt failed")
			var output bytes.Buffer

			written, err := decryptStream(context.Background(), &output, bytes.NewReader([]byte("encrypted")), func([]byte) error {
				return decryptErr
			})

			Expect(written).To(BeZero())
			Expect(output.Len()).To(BeZero())
			Expect(err).To(MatchError(decryptErr))
		})

		It("reports short writes", func() {
			written, err := decryptStream(context.Background(), shortWriter{}, bytes.NewReader([]byte("encrypted")), func([]byte) error {
				return nil
			})

			Expect(written).To(Equal(int64(1)))
			Expect(err).To(MatchError(ContainSubstring(io.ErrShortWrite.Error())))
		})
	})

	It("closes the session and machine idempotently", func() {
		machine := newGinkgoServiceMachine(shimOptions{})
		var closedSession uint64
		closeEntry, err := machine.services.addFunction("test.storeagent.close", func() error {
			value, argumentErr := machine.services.argument(0)
			if argumentErr != nil {
				return argumentErr
			}

			closedSession = value

			return machine.services.setResult(0)
		})
		Expect(err).NotTo(HaveOccurred())

		agent := &StoreAgent{guest: machine, session: 0x1234, closeEntry: closeEntry}
		Expect(agent.Close()).To(Succeed())
		Expect(closedSession).To(Equal(uint64(0x1234)))
		Expect(agent.session).To(BeZero())
		Expect(agent.guest).To(BeNil())
		Expect(agent.Close()).To(Succeed())
	})
})

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) {
	return 1, nil
}

func newGinkgoServiceMachine(options shimOptions) *Machine {
	engine, err := unicorn.New(context.Background())
	Expect(err).NotTo(HaveOccurred())

	for _, region := range []struct {
		address uint64
		size    uint64
	}{
		{returnAddress, pageSize},
		{scratchBase, scratchSize},
		{heapBase, heapSize},
		{stackBase, stackSize},
	} {
		Expect(engine.MemMap(region.address, region.size)).To(Succeed())
	}

	Expect(engine.MemWrite(returnAddress, []byte{0xF4})).To(Succeed())

	services, err := newShimsWithOptions(engine, map[string]uint64{}, nil, options)
	Expect(err).NotTo(HaveOccurred())

	machine := &Machine{engine: engine, services: services}

	DeferCleanup(func() {
		Expect(machine.Close()).To(Succeed())
	})

	return machine
}
