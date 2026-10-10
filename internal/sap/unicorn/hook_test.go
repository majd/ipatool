package unicorn

import "testing"

func TestCodeHookStopsEmulation(t *testing.T) {
	engine := newTestEngine(t)
	defer engine.Close()

	const address = uint64(0x2000000)
	if err := engine.MemMap(address, 0x1000); err != nil {
		t.Fatal(err)
	}

	// mov x0, #1; add x0, x0, #1; brk #0
	code := []byte{0x20, 0x00, 0x80, 0xd2, 0x00, 0x04, 0x00, 0x91, 0x00, 0x00, 0x20, 0xd4}
	if err := engine.MemWrite(address, code); err != nil {
		t.Fatal(err)
	}

	var (
		calls       int
		hookAddress uint64
		hookSize    uint32
		stopErr     error
	)

	hook, err := engine.AddCodeHook(address+4, address+4, func(current uint64, size uint32) {
		calls++
		hookAddress = current
		hookSize = size
		stopErr = engine.Stop()
	})
	if err != nil {
		t.Fatal(err)
	}

	defer hook.Close()

	if err := engine.Start(address, address+uint64(len(code))); err != nil {
		t.Fatal(err)
	}

	if stopErr != nil {
		t.Fatal(stopErr)
	}

	if calls != 1 {
		t.Fatalf("callback calls = %d, want 1", calls)
	}

	if hookAddress != address+4 || hookSize != 4 {
		t.Fatalf("callback instruction = (%#x, %d), want (%#x, 4)", hookAddress, hookSize, address+4)
	}
}

func TestClosedCodeHookDoesNotRun(t *testing.T) {
	engine := newTestEngine(t)
	defer engine.Close()

	const address = uint64(0x3000000)
	if err := engine.MemMap(address, 0x1000); err != nil {
		t.Fatal(err)
	}

	if err := engine.MemWrite(address, []byte{0x1f, 0x20, 0x03, 0xd5}); err != nil {
		t.Fatal(err)
	}

	called := false

	hook, err := engine.AddCodeHook(address, address, func(uint64, uint32) {
		called = true
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := hook.Close(); err != nil {
		t.Fatal(err)
	}

	if err := hook.Close(); err != nil {
		t.Fatal(err)
	}

	if err := engine.Start(address, address+4); err != nil {
		t.Fatal(err)
	}

	if called {
		t.Fatal("closed callback ran")
	}
}

func TestHookCloseIsSafeDuringEngineClose(t *testing.T) {
	hookDeleteStarted := make(chan struct{})
	releaseHookDelete := make(chan struct{})
	engine := &Engine{
		handle:    1,
		closeDone: make(chan struct{}),
		api: api{
			hookAdd: func(_ uintptr, handle *uintptr, _ int32, _, _ uintptr, _, _ uint64) int32 {
				*handle = 2

				return 0
			},
			hookDel: func(uintptr, uintptr) int32 {
				close(hookDeleteStarted)
				<-releaseHookDelete

				return 0
			},
			close: func(uintptr) int32 { return 0 },
		},
	}

	hook, err := engine.AddCodeHook(0, 1, func(uint64, uint32) {})
	if err != nil {
		t.Fatal(err)
	}

	id := hook.id

	hookErr := make(chan error, 1)
	go func() { hookErr <- hook.Close() }()
	<-hookDeleteStarted

	closeErr := make(chan error, 1)

	go func() { closeErr <- engine.Close() }()
	waitForEngineClosing(t, engine)

	close(releaseHookDelete)
	waitForResult(t, hookErr)
	waitForResult(t, closeErr)

	if _, exists := codeHookCallbacks.Load(id); exists {
		t.Fatal("closed hook callback remains registered")
	}

	if err := hook.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHookCloseAfterEngineClose(t *testing.T) {
	engine := &Engine{
		handle:    1,
		closeDone: make(chan struct{}),
		api: api{
			hookAdd: func(_ uintptr, handle *uintptr, _ int32, _, _ uintptr, _, _ uint64) int32 {
				*handle = 2

				return 0
			},
			close: func(uintptr) int32 { return 0 },
		},
	}

	hook, err := engine.AddCodeHook(0, 1, func(uint64, uint32) {})
	if err != nil {
		t.Fatal(err)
	}

	id := hook.id

	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}

	if err := hook.Close(); err != nil {
		t.Fatal(err)
	}

	if _, exists := codeHookCallbacks.Load(id); exists {
		t.Fatal("closed hook callback remains registered")
	}
}
