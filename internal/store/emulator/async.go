package emulator

import (
	"github.com/majd/ipatool/v2/internal/sap/unicorn"
)

type asyncTask struct {
	continuation                            uint64
	handle, context, entry, closure, result uint64
	executor                                [2]uint64
	done                                    bool
}

func (r *Runtime) asyncSymbol(name string) bool {
	switch name {
	case "swift_task_localValueGet":
		r.set(r.taskLocalValue(r.arg(0)))
	case "swift_task_localValuePush":
		key, source, metadata := r.arg(0), r.arg(1), r.arg(2)
		size := r.read64(r.read64(metadata-8) + 64)
		value := r.alloc(size)

		data, err := r.engine.MemRead(source, size)
		if err != nil {
			panic(err)
		}

		if err = r.MemWrite(value, data); err != nil {
			panic(err)
		}

		if r.taskLocalFrames == nil {
			r.taskLocalFrames = map[uint64][]taskLocalBinding{}
		}

		id := r.taskLocalOwner()
		r.taskLocalFrames[id] = append(r.taskLocalFrames[id], taskLocalBinding{key, value})
		r.set(0)
	case "swift_task_localValuePop":
		id := r.taskLocalOwner()

		values := r.taskLocalFrames[id]
		if len(values) == 0 {
			panic("empty TaskLocal binding stack")
		}

		r.taskLocalFrames[id] = values[:len(values)-1]
	case "swift_task_localsCopyTo":
		if r.tasks[r.arg(0)] == nil {
			panic("unknown task for local bindings")
		}

		if r.taskLocalFrames == nil {
			r.taskLocalFrames = map[uint64][]taskLocalBinding{}
		}

		r.taskLocalFrames[r.arg(0)] = append([]taskLocalBinding(nil), r.taskLocalFrames[r.taskLocalOwner()]...)

	case "swift_continuation_init":
		if r.currentTask == nil {
			panic("continuation initialized outside task")
		}

		context, flags := r.arg(0), r.arg(1)
		if flags&4 != 0 {
			panic("preawaited continuation unsupported")
		}

		r.currentTask.continuation = context
		r.write64(context+16, (flags&1)|((flags&8)>>2))
		r.write64(context+24, 0)
		r.write64(context+32, 0)

		if flags&2 == 0 {
			r.write64(context+48, 0)
			r.write64(context+56, 0)
		}

		r.set(r.currentTask.handle)
	case "swift_continuation_resume", "swift_continuation_throwingResume", "swift_continuation_throwingResumeWithError":
		task := r.tasks[r.arg(0)]
		if task == nil || task.continuation == 0 {
			panic("unknown Swift continuation")
		}

		context := task.continuation
		if r.read64(context+24) == 2 {
			panic("Swift continuation resumed twice")
		}

		if name == "swift_continuation_throwingResumeWithError" {
			r.write64(context+32, r.arg(1))
		}

		r.write64(context+24, 2)
		r.set(0)
	case "swift_continuation_await":
		context := r.arg(0)
		task := r.currentTask

		if task == nil || task.continuation != context {
			panic("awaiting unknown Swift continuation")
		}

		var resume func()
		resume = func() {
			if r.read64(context+24) == 2 {
				task.continuation = 0
				r.currentTask = task
				_ = r.engine.RegWrite(unicorn.RegX0+22, context)
				_ = r.engine.RegWrite(unicorn.RegPC, r.read64(context+8))

				return
			}

			block := r.nextPending()
			if block == 0 {
				panic("Swift continuation has no runnable completion")
			}

			r.currentTask = nil
			r.pendingCallback(block, func() { r.currentTask = task; resume() })
		}
		resume()

	case "swift_task_create":
		flags, options, resultType, descriptor, closure := r.arg(0), r.arg(1), r.arg(2), r.arg(3), r.arg(4)
		if options != 0 || flags&^uint64(0x1cff) != 0 {
			panic("unsupported Swift task options")
		}

		bits := r.read64(descriptor)

		size := bits >> 32
		if size < 16 || size > 1<<20 {
			panic("invalid Swift async context size")
		}

		task := &asyncTask{handle: r.New("HostSwiftTask", nil), context: r.alloc(size+32) + 32, entry: uint64(int64(descriptor) + int64(int32(bits))), closure: closure}

		valueSize := uint64(0)
		if resultType != 0 {
			valueSize = r.read64(r.read64(resultType-8) + 64)
		}

		task.result = r.alloc(valueSize)

		if r.tasks == nil {
			r.tasks = map[uint64]*asyncTask{}
		}

		r.tasks[task.handle] = task
		if flags&(1<<10) != 0 {
			if r.taskLocalFrames == nil {
				r.taskLocalFrames = map[uint64][]taskLocalBinding{}
			}

			r.taskLocalFrames[task.handle] = append([]taskLocalBinding(nil), r.taskLocalFrames[r.taskLocalOwner()]...)
		}

		r.pending = append(r.pending, r.hostBlock(func() { r.startAsync(task) }))
		r.set(task.handle)
		_ = r.engine.RegWrite(unicorn.RegX1, task.context)
	case "swift_task_alloc":
		r.set(r.alloc(r.arg(0)))
	case "swift_task_dealloc":
	case "swift_task_getCurrent":
		if r.currentTask == nil {
			r.set(0)
		} else {
			r.set(r.currentTask.handle)
		}
	case "swift_task_isCancelled":
		r.set(boolValue(r.ctx.Err() != nil))
	case "swift_task_switch":
		if r.currentTask == nil {
			panic("executor switch outside a Swift task")
		}

		function := r.arg(0)
		r.currentTask.executor = [2]uint64{r.arg(1), r.arg(2)}
		_ = r.engine.RegWrite(unicorn.RegPC, function)
	case "swift_task_getCurrentExecutor":
		executor := [2]uint64{}
		if r.currentTask != nil {
			executor = r.currentTask.executor
		}

		r.set(executor[0])
		_ = r.engine.RegWrite(unicorn.RegX1, executor[1])
	case "swift_task_isCurrentExecutor":
		executor := [2]uint64{}
		if r.currentTask != nil {
			executor = r.currentTask.executor
		}

		r.set(boolValue(executor == [2]uint64{r.arg(0), r.arg(1)}))
	default:
		return false
	}

	return true
}
func (r *Runtime) startAsync(task *asyncTask) {
	if task.done {
		panic("attempt to restart a completed Swift task")
	}

	saved := map[int]uint64{}

	for _, reg := range []int{unicorn.RegSP, unicorn.RegLR, unicorn.RegX29, unicorn.RegX0 + 19, unicorn.RegX0 + 20, unicorn.RegX0 + 21, unicorn.RegX0 + 22, unicorn.RegX0 + 23, unicorn.RegX0 + 24, unicorn.RegX0 + 25, unicorn.RegX0 + 26, unicorn.RegX0 + 27, unicorn.RegX0 + 28} {
		v, err := r.engine.RegRead(reg)
		if err != nil {
			panic(err)
		}

		saved[reg] = v
	}

	previous := r.currentTask
	r.currentTask = task
	completion := r.alloc(16)

	if err := r.MemWrite(completion, []byte{0xc0, 0x03, 0x5f, 0xd6}); err != nil {
		panic(err)
	}

	r.callbacks[completion] = func() {
		if task.done {
			panic("Swift task completed twice")
		}

		task.done = true

		r.currentTask = previous
		for reg, v := range saved {
			if err := r.engine.RegWrite(reg, v); err != nil {
				panic(err)
			}
		}

		r.set(0)
		_ = r.engine.RegWrite(unicorn.RegPC, saved[unicorn.RegLR])
	}
	r.write64(task.context, 0)
	r.write64(task.context+8, completion)
	r.write64(task.context-32, task.result)
	r.write64(task.context-24, task.entry)
	r.write64(task.context-16, task.closure)
	_ = r.engine.RegWrite(unicorn.RegX0, task.result)
	_ = r.engine.RegWrite(unicorn.RegX0+20, task.closure)
	_ = r.engine.RegWrite(unicorn.RegX0+21, 0)
	_ = r.engine.RegWrite(unicorn.RegX0+22, task.context)
	_ = r.engine.RegWrite(unicorn.RegPC, task.entry)
}

func (r *Runtime) taskLocalOwner() uint64 {
	if r.currentTask != nil {
		return r.currentTask.handle
	}

	return 0
}
func (r *Runtime) taskLocalValue(key uint64) uint64 {
	frames := r.taskLocalFrames[r.taskLocalOwner()]
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i].key == key {
			return frames[i].value
		}
	}

	return 0
}
