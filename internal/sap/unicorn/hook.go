package unicorn

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/ebitengine/purego"
)

const hookCode = 1 << 2

type CodeHook func(address uint64, size uint32)

type hookState struct {
	mu  sync.Mutex
	ids map[uintptr]struct{}
}

type Hook struct {
	mu     sync.Mutex
	engine *Engine
	handle uintptr
	id     uintptr
}

var (
	codeHookID         atomic.Uint64
	codeHookCallbacks  sync.Map
	codeHookTrampoline = purego.NewCallback(func(
		_ purego.CDecl,
		_ uintptr,
		address uint64,
		size uint32,
		userData uintptr,
	) uintptr {
		callback, ok := codeHookCallbacks.Load(userData)
		if ok {
			callback.(CodeHook)(address, size)
		}

		return 0
	})
)

// AddCodeHook registers callback for instructions whose start address is in
// the inclusive range [begin, end]. The callback is retained until the Hook or
// Engine is closed; it runs through a C ABI trampoline and must not panic.
func (e *Engine) AddCodeHook(begin, end uint64, callback CodeHook) (*Hook, error) {
	handle, done, err := e.beginOperation()
	if err != nil {
		return nil, err
	}

	defer done()

	if callback == nil {
		return nil, errors.New("unicorn code hook callback is nil")
	}

	id := uintptr(codeHookID.Add(1))
	if id == 0 {
		id = uintptr(codeHookID.Add(1))
	}

	codeHookCallbacks.Store(id, callback)

	var hookHandle uintptr
	if err := e.err(e.api.hookAdd(handle, &hookHandle, hookCode, codeHookTrampoline, id, begin, end)); err != nil {
		codeHookCallbacks.Delete(id)

		return nil, err
	}

	e.hooks.mu.Lock()
	if e.hooks.ids == nil {
		e.hooks.ids = make(map[uintptr]struct{})
	}

	e.hooks.ids[id] = struct{}{}
	e.hooks.mu.Unlock()

	return &Hook{engine: e, handle: hookHandle, id: id}, nil
}

func (h *Hook) Close() error {
	if h == nil {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.engine == nil {
		return nil
	}

	engine := h.engine
	handle, done, err := engine.beginOperation()

	if err == nil {
		err = engine.err(engine.api.hookDel(handle, h.handle))

		done()

		if err != nil {
			return err
		}
	} else if !errors.Is(err, errEngineClosed) {
		return err
	}

	codeHookCallbacks.Delete(h.id)
	engine.hooks.mu.Lock()
	delete(engine.hooks.ids, h.id)
	engine.hooks.mu.Unlock()

	h.engine = nil
	h.handle = 0
	h.id = 0

	return nil
}

func (e *Engine) clearCodeHooks() {
	e.hooks.mu.Lock()
	defer e.hooks.mu.Unlock()

	for id := range e.hooks.ids {
		codeHookCallbacks.Delete(id)
	}

	clear(e.hooks.ids)
}
