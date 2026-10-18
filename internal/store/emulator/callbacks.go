package emulator

import "github.com/majd/ipatool/v2/internal/sap/unicorn"

func (r *Runtime) callback(block uint64, args []uint64, after func()) {
	r.callbackFunction(r.read64(block+16), append([]uint64{block}, args...), after)
}
func (r *Runtime) callbackFunction(function uint64, args []uint64, after func()) {
	link, err := r.engine.RegRead(unicorn.RegLR)
	if err != nil {
		panic(err)
	}

	resume := r.alloc(16)
	if err = r.MemWrite(resume, []byte{0xc0, 0x03, 0x5f, 0xd6}); err != nil {
		panic(err)
	}

	r.callbacks[resume] = func() {
		delete(r.callbacks, resume)
		_ = r.engine.RegWrite(unicorn.RegLR, link)
		_ = r.engine.RegWrite(unicorn.RegPC, link)

		after()
	}
	for i, v := range args {
		if err = r.engine.RegWrite(unicorn.RegX0+i, v); err != nil {
			panic(err)
		}
	}

	_ = r.engine.RegWrite(unicorn.RegLR, resume)
	_ = r.engine.RegWrite(unicorn.RegPC, function)
}

func (r *Runtime) copyBlock(block uint64, after func(uint64)) {
	if block == 0 {
		after(0)

		return
	}

	flags := uint32(r.read64(block + 8))
	if flags&(1<<28) != 0 {
		after(block)

		return
	}

	descriptor := r.read64(block + 24)

	size := r.read64(descriptor + 8)
	if size < 32 || size > 1<<20 {
		panic("invalid guest block size")
	}

	copied := r.alloc(size)

	data, err := r.engine.MemRead(block, size)
	if err != nil {
		panic(err)
	}

	if err = r.MemWrite(copied, data); err != nil {
		panic(err)
	}

	if flags&(1<<25) != 0 {
		r.callbackFunction(r.read64(descriptor+16), []uint64{copied, block}, func() { after(copied) })

		return
	}

	after(copied)
}

func (r *Runtime) copyValue(value uint64, after func(uint64)) {
	if value == 0 {
		after(0)

		return
	}

	if obj := r.objects[value]; obj != nil {
		if entry := r.lookup(obj.Class, "copyWithZone:", false); entry != 0 {
			r.callbackFunction(entry, []uint64{value, r.selector("copyWithZone:"), 0}, func() { after(r.arg(0)) })

			return
		}

		after(value)

		return
	}

	r.copyBlock(value, after)
}

func (r *Runtime) copyByref(value uint64, after func(uint64)) {
	forwarded := r.read64(value + 8)
	if forwarded != 0 && forwarded != value {
		after(forwarded)

		return
	}

	header := r.read64(value + 16)
	size := header >> 32
	flags := uint32(header)

	if size < 24 || size > 1<<20 {
		panic("invalid byref capture size")
	}

	copied := r.alloc(size)

	data, err := r.engine.MemRead(value, size)
	if err != nil {
		panic(err)
	}

	if err = r.MemWrite(copied, data); err != nil {
		panic(err)
	}

	r.write64(copied+8, copied)
	r.write64(value+8, copied)

	if flags&(1<<25) != 0 {
		r.callbackFunction(r.read64(value+24), []uint64{copied, value}, func() { after(copied) })

		return
	}

	after(copied)
}
