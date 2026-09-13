//go:build darwin || linux

package unicorn

import (
	"debug/elf"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLibcDetection(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Unicorn Libc Detection Suite")
}

var _ = Describe("Linux libc detection", func() {
	DescribeTable("classifies interpreter names", func(interpreter string, expected bool) {
		Expect(interpreterUsesMusl(interpreter)).To(Equal(expected))
	},
		Entry("musl amd64", "/lib/ld-musl-x86_64.so.1", true),
		Entry("musl arm64", "/lib/ld-musl-aarch64.so.1", true),
		Entry("glibc amd64", "/lib64/ld-linux-x86-64.so.2", false),
		Entry("glibc arm64", "/lib/ld-linux-aarch64.so.1", false),
		Entry("glibc under usr", "/usr/lib/ld-linux-x86-64.so.2", false),
		Entry("empty", "", false),
	)

	DescribeTable("prefers the executable over installed loaders", func(interpreter string, installed, expected bool) {
		path := filepath.Join(GinkgoT().TempDir(), "exe")
		Expect(os.WriteFile(path, libcTestELF(interpreter), 0o600)).To(Succeed())

		actual, ok := executableInterpreter(path)
		Expect(ok).To(BeTrue())
		Expect(actual).To(Equal(interpreter))

		fallbackCalled := false
		Expect(linuxUsesMuslFor(path, func() bool {
			fallbackCalled = true

			return installed
		})).To(Equal(expected))
		Expect(fallbackCalled).To(BeFalse())
	},
		Entry("glibc with musl installed", "/lib64/ld-linux-x86-64.so.2", true, false),
		Entry("glibc arm64 with musl installed", "/lib/ld-linux-aarch64.so.1", true, false),
		Entry("musl without a standard loader path", "/custom/ld-musl-x86_64.so.1", false, true),
		Entry("musl arm64", "/lib/ld-musl-aarch64.so.1", true, true),
	)

	DescribeTable("falls back when the interpreter is unavailable", func(kind string) {
		path := filepath.Join(GinkgoT().TempDir(), "exe")
		switch kind {
		case "static":
			Expect(os.WriteFile(path, libcTestELF(""), 0o600)).To(Succeed())
		case "invalid":
			Expect(os.WriteFile(path, []byte("not an ELF executable"), 0o600)).To(Succeed())
		case "truncated":
			data := libcTestELF("/lib/ld-musl-x86_64.so.1")
			Expect(os.WriteFile(path, data[:len(data)-4], 0o600)).To(Succeed())
		}

		interpreter, ok := executableInterpreter(path)
		Expect(ok).To(BeFalse())
		Expect(interpreter).To(BeEmpty())
		for _, installed := range []bool{false, true} {
			fallbackCalled := false
			Expect(linuxUsesMuslFor(path, func() bool {
				fallbackCalled = true

				return installed
			})).To(Equal(installed))
			Expect(fallbackCalled).To(BeTrue())
		}
	},
		Entry("no PT_INTERP", "static"),
		Entry("executable cannot be opened", "missing"),
		Entry("invalid ELF", "invalid"),
		Entry("interpreter cannot be read", "truncated"),
	)

	It("reads the running Linux executable", func() {
		if runtime.GOOS != "linux" {
			Skip("/proc/self/exe is Linux-specific")
		}
		interpreter, ok := executableInterpreter("/proc/self/exe")
		if !ok {
			Skip("running executable has no readable PT_INTERP")
		}
		Expect(filepath.IsAbs(interpreter)).To(BeTrue())
		Expect(linuxUsesMusl()).To(Equal(interpreterUsesMusl(interpreter)))
	})
})

// libcTestELF builds an ELF64 executable with one program header. An empty
// interpreter uses PT_NULL to model a static executable without PT_INTERP.
func libcTestELF(interpreter string) []byte {
	const headerSize, programSize = 64, 56
	data := make([]byte, headerSize+programSize)
	copy(data, "\x7fELF")
	data[elf.EI_CLASS] = byte(elf.ELFCLASS64)
	data[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	data[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	binary.LittleEndian.PutUint16(data[16:], uint16(elf.ET_EXEC))
	binary.LittleEndian.PutUint16(data[18:], uint16(elf.EM_X86_64))
	binary.LittleEndian.PutUint32(data[20:], uint32(elf.EV_CURRENT))
	binary.LittleEndian.PutUint64(data[32:], headerSize)  // e_phoff
	binary.LittleEndian.PutUint16(data[52:], headerSize)  // e_ehsize
	binary.LittleEndian.PutUint16(data[54:], programSize) // e_phentsize
	binary.LittleEndian.PutUint16(data[56:], 1)           // e_phnum

	if interpreter != "" {
		binary.LittleEndian.PutUint32(data[headerSize:], uint32(elf.PT_INTERP))
		binary.LittleEndian.PutUint64(data[headerSize+8:], headerSize+programSize)      // p_offset
		binary.LittleEndian.PutUint64(data[headerSize+32:], uint64(len(interpreter)+1)) // p_filesz
		data = append(data, interpreter...)
		data = append(data, 0)
	}

	return data
}
