package unicorn

import (
	"context"
	"runtime"

	"github.com/ebitengine/purego"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Translation buffer limit", func() {
	It("sets the native buffer size before guest memory is mapped", func() {
		called := false
		engine := &Engine{handle: 123, api: api{ctl: func(handle uintptr, control, value uint32) int32 {
			called = true
			Expect(handle).To(Equal(uintptr(123)))
			Expect(control).To(Equal(uint32(13 | 1<<26 | 1<<30)))
			Expect(value).To(Equal(uint32(tcgBufferSize)))

			return 0
		}}}
		Expect(configureEngine(engine)).To(Succeed())
		Expect(called).To(BeTrue())
	})
	It("applies the limit to a real engine before native initialization", func() {
		engine, err := New(context.Background())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(engine.Close)
		Expect(engine.MemMap(0x1000, 0x1000)).To(Succeed())
		var readBufferSize func(uintptr, uint32, *uint32) int32
		if (runtime.GOOS == "darwin" || runtime.GOOS == "ios") && runtime.GOARCH == "arm64" {
			var control func(uintptr, uint32, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, *uint32) int32
			purego.RegisterLibFunc(&control, engine.library.handle, "uc_ctl")
			readBufferSize = func(engine uintptr, command uint32, value *uint32) int32 {
				return control(engine, command, 0, 0, 0, 0, 0, 0, value)
			}
		} else {
			purego.RegisterLibFunc(&readBufferSize, engine.library.handle, "uc_ctl")
		}
		var size uint32
		Expect(engine.err(readBufferSize(engine.handle, 13|1<<26|2<<30, &size))).To(Succeed())
		Expect(size).To(Equal(uint32(tcgBufferSize)))
		Expect(engine.err(readBufferSize(engine.handle, 7|1<<26|2<<30, &size))).To(Succeed())
		Expect(size).To(Equal(arm64CPUModelMax))
	})

	It("reports failure to set the native buffer size", func() {
		engine := &Engine{api: api{
			ctl:      func(uintptr, uint32, uint32) int32 { return 15 },
			strerror: func(int32) string { return "invalid argument" },
		}}
		Expect(configureEngine(engine)).To(MatchError(ContainSubstring("configure Unicorn translation buffer")))
	})
})
