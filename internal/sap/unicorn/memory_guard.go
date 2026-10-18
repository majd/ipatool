package unicorn

import (
	"errors"
	"sync"

	"github.com/ebitengine/purego"
)

const hookMemoryAccess = 1<<10 | 1<<11

// memoryGuard records the first guest violation even when uc_emu_stop returns a
// successful status. Host accesses return their validation errors directly.
type memoryGuard struct {
	check func(address, size uint64) error
	mu    sync.Mutex
	err   error
}

var memoryAccessTrampoline = purego.NewCallback(func(
	_ purego.CDecl, _ uintptr, _ int32, address uint64, size int32, _ int64, userData uintptr,
) uintptr {
	if callback, ok := codeHookCallbacks.Load(userData); ok {
		callback.(CodeHook)(address, uint32(size))
	}

	return 0
})

// GuardMemory validates guest reads, writes and instructions as well as host
// MemRead, MemReadInto and MemWrite calls. Install it once, before executing any
// guest code, without concurrent engine operations. The check must not panic.
// Hooks live until Close; a rejected guest access permanently fails execution.
func (e *Engine) GuardMemory(check func(address, size uint64) error) error {
	if check == nil {
		return errors.New("unicorn memory validator is nil")
	}

	if e.memoryGuard != nil {
		return errors.New("unicorn memory validator is already installed")
	}

	guard := &memoryGuard{check: check}
	callback := CodeHook(func(address uint64, size uint32) {
		if err := check(address, uint64(size)); err != nil {
			guard.mu.Lock()
			if guard.err == nil {
				guard.err = err
			}
			guard.mu.Unlock()

			_ = e.Stop()
		}
	})

	dataHook, err := e.addHook(hookMemoryAccess, memoryAccessTrampoline, 1, 0, callback)
	if err != nil {
		return err
	}

	// Instruction hooks also cover fetches served from Unicorn's translation
	// cache, which need not trigger a memory-fetch hook on subsequent execution.
	if _, err = e.AddCodeHook(1, 0, callback); err != nil {
		_ = dataHook.Close()

		return err
	}

	e.memoryGuard = guard

	return nil
}

// checkMemory applies the same policy to accesses made by the host bridge.
func (e *Engine) checkMemory(address, size uint64) error {
	if e.memoryGuard != nil && size != 0 {
		return e.memoryGuard.check(address, size)
	}

	return nil
}

// executionError preserves guard failures even when Unicorn stops successfully.
func (e *Engine) executionError(native error) error {
	if e.memoryGuard != nil {
		e.memoryGuard.mu.Lock()
		defer e.memoryGuard.mu.Unlock()

		if e.memoryGuard.err != nil {
			return errors.Join(e.memoryGuard.err, native)
		}
	}

	return native
}
