package emulator

import (
	"bytes"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type pageWrite struct {
	address uint64
	data    []byte
}
type pageMemory struct {
	writes []pageWrite
	fail   error
}

func (m *pageMemory) MemWrite(address uint64, data []byte) error {
	if m.fail != nil {
		return m.fail
	}

	m.writes = append(m.writes, pageWrite{address, bytes.Clone(data)})

	return nil
}

var _ = Describe("Store profile page transfers", func() {
	It("batches consecutive source and guest pages without writing gaps", func() {
		data := make([]byte, 3*profilePageSize)
		for n := range data {
			data[n] = byte(n/4096 + 1)
		}
		memory := &pageMemory{}
		Expect(writeProfilePages(memory, []Page{{Address: 0x1000, Offset: 0}, {Address: 0x2000, Offset: 4096}, {Address: 0x9000, Offset: 8192}}, data)).To(Succeed())
		Expect(memory.writes).To(HaveLen(2))
		Expect(memory.writes[0]).To(Equal(pageWrite{0x1000, data[:8192]}))
		Expect(memory.writes[1]).To(Equal(pageWrite{0x9000, data[8192:]}))
	})
	It("preserves writes when source pages or guest addresses are out of order", func() {
		data := make([]byte, 3*profilePageSize)
		memory := &pageMemory{}
		pages := []Page{{Address: 0x2000, Offset: 0}, {Address: 0x1000, Offset: 8192}, {Address: 0x2000, Offset: 4096}}
		Expect(writeProfilePages(memory, pages, data)).To(Succeed())
		Expect(memory.writes).To(HaveLen(3))
		for n, page := range pages {
			Expect(memory.writes[n]).To(Equal(pageWrite{page.Address, data[page.Offset : page.Offset+4096]}))
		}
	})
	It("rejects truncated and wrapping source ranges", func() {
		for _, offset := range []uint64{1, 4096, ^uint64(0) - 4095} {
			memory := &pageMemory{}
			Expect(writeProfilePages(memory, []Page{{Address: 0x1000, Offset: offset}}, make([]byte, 4096))).To(MatchError("profile page exceeds data"))
			Expect(memory.writes).To(BeEmpty())
		}
	})
	It("does not merge across a wrapping guest address", func() {
		memory := &pageMemory{}
		Expect(writeProfilePages(memory, []Page{{Address: ^uint64(0) - 4095, Offset: 0}, {Address: 0, Offset: 4096}}, make([]byte, 8192))).To(Succeed())
		Expect(memory.writes).To(HaveLen(2))
	})
	It("preserves memory write errors", func() {
		failed := errors.New("write failure")
		err := writeProfilePages(&pageMemory{fail: failed}, []Page{{Address: 0x1000}}, make([]byte, 4096))
		Expect(errors.Is(err, failed)).To(BeTrue())
	})
})
