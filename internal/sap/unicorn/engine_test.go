package unicorn

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewPassesContextToLibraryLoader(t *testing.T) {
	type contextKey struct{}

	ctx := context.WithValue(context.Background(), contextKey{}, "value")
	wantErr := errors.New("loader error")
	called := false
	_, err := newEngine(ctx, func(received context.Context) (library, error) {
		called = true

		if received.Value(contextKey{}) != "value" {
			t.Fatal("library loader did not receive constructor context")
		}

		return library{}, wantErr
	})

	if !called {
		t.Fatal("library loader was not called")
	}

	if !errors.Is(err, wantErr) {
		t.Fatalf("New error = %v, want %v", err, wantErr)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	called = false

	_, err = newEngine(canceled, func(context.Context) (library, error) {
		called = true

		return library{}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("New canceled error = %v, want %v", err, context.Canceled)
	}

	if called {
		t.Fatal("library loader ran for an already canceled context")
	}

	duringLoad, cancelDuringLoad := context.WithCancel(context.Background())
	libraryClosed := false

	_, err = newEngine(duringLoad, func(context.Context) (library, error) {
		cancelDuringLoad()

		return library{
			handle: 1,
			close: func() error {
				libraryClosed = true

				return nil
			},
		}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("New mid-load cancellation error = %v, want %v", err, context.Canceled)
	}

	if !libraryClosed {
		t.Fatal("library loaded during cancellation was not closed")
	}
}

func TestEngineOperationsFailAfterClose(t *testing.T) {
	engine := &Engine{
		handle:    1,
		closeDone: make(chan struct{}),
		api: api{
			close: func(uintptr) int32 { return 0 },
		},
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}

	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		call func() error
	}{
		{"MemMap", func() error { return engine.MemMap(0, 0x1000) }},
		{"MemUnmap", func() error { return engine.MemUnmap(0, 0x1000) }},
		{"MemRead", func() error {
			_, err := engine.MemRead(0, 0)

			return err
		}},
		{"MemReadInto", func() error { return engine.MemReadInto(nil, 0) }},
		{"MemWrite", func() error { return engine.MemWrite(0, nil) }},
		{"RegRead", func() error {
			_, err := engine.RegRead(RegRAX)

			return err
		}},
		{"RegWrite", func() error { return engine.RegWrite(RegRAX, 0) }},
		{"Start", func() error { return engine.Start(0, 1) }},
		{"StartBounded", func() error { return engine.StartBounded(0, 1, time.Second, 1) }},
		{"Stop", engine.Stop},
		{"AddCodeHook", func() error {
			_, err := engine.AddCodeHook(0, 1, func(uint64, uint32) {})

			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !errors.Is(err, errEngineClosed) {
				t.Fatalf("error = %v, want %v", err, errEngineClosed)
			}
		})
	}
}

func TestConcurrentCloseWaitsForActiveOperation(t *testing.T) {
	operationStarted := make(chan struct{})
	releaseOperation := make(chan struct{})

	var closeCalls atomic.Int32

	engine := &Engine{
		handle:    1,
		closeDone: make(chan struct{}),
		api: api{
			memMap: func(uintptr, uint64, uint64, uint32) int32 {
				close(operationStarted)
				<-releaseOperation

				return 0
			},
			close: func(uintptr) int32 {
				closeCalls.Add(1)

				return 0
			},
		},
	}

	operationErr := make(chan error, 1)
	go func() {
		operationErr <- engine.MemMap(0x1000, 0x1000)
	}()
	<-operationStarted

	firstClose := make(chan error, 1)
	secondClose := make(chan error, 1)

	go func() { firstClose <- engine.Close() }()
	waitForEngineClosing(t, engine)

	go func() { secondClose <- engine.Close() }()

	if closeCalls.Load() != 0 {
		t.Fatal("native engine closed while an operation was active")
	}

	close(releaseOperation)
	waitForResult(t, operationErr)
	waitForResult(t, firstClose)
	waitForResult(t, secondClose)

	if closeCalls.Load() != 1 {
		t.Fatalf("native close calls = %d, want 1", closeCalls.Load())
	}
}

func waitForEngineClosing(t *testing.T, engine *Engine) {
	t.Helper()

	deadline := time.Now().Add(time.Second)

	for {
		engine.stateMu.Lock()
		closing := engine.closing
		engine.stateMu.Unlock()

		if closing {
			return
		}

		if time.Now().After(deadline) {
			t.Fatal("engine did not begin closing")
		}

		runtime.Gosched()
	}
}

func waitForResult(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("operation did not finish")
	}
}

func TestEngineExecutesX8664(t *testing.T) {
	engine := newTestEngine(t)
	defer engine.Close()

	const address = uint64(0x1000000)
	if err := engine.MemMap(address, 0x1000); err != nil {
		t.Fatal(err)
	}

	// mov rax, 0x1234; hlt
	code := []byte{0x48, 0xc7, 0xc0, 0x34, 0x12, 0x00, 0x00, 0xf4}
	if err := engine.MemWrite(address, code); err != nil {
		t.Fatal(err)
	}

	if err := engine.Start(address, address+uint64(len(code))); err != nil {
		t.Fatal(err)
	}

	value, err := engine.RegRead(RegRAX)
	if err != nil {
		t.Fatal(err)
	}

	if value != 0x1234 {
		t.Fatalf("RAX = %#x, want %#x", value, uint64(0x1234))
	}
}

func TestStartBoundedReportsTimeout(t *testing.T) {
	engine := newTestEngine(t)
	defer engine.Close()

	const address = uint64(0x1100000)
	if err := engine.MemMap(address, 0x1000); err != nil {
		t.Fatal(err)
	}

	// jmp $-2
	if err := engine.MemWrite(address, []byte{0xeb, 0xfe}); err != nil {
		t.Fatal(err)
	}

	err := engine.StartBounded(address, address+2, 10*time.Millisecond, 0)
	if !errors.Is(err, errTimeout) {
		t.Fatalf("StartBounded error = %v, want %v", err, errTimeout)
	}

	const completionAddress = address + 0x10
	if err := engine.MemWrite(completionAddress, []byte{0xf4}); err != nil {
		t.Fatal(err)
	}

	if err := engine.StartBounded(completionAddress, completionAddress+1, time.Second, 0); err != nil {
		t.Fatalf("StartBounded after timeout: %v", err)
	}
}
