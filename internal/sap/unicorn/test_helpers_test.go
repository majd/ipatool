package unicorn

import (
	"context"
	"testing"
)

func newTestEngine(t *testing.T) *Engine {
	t.Helper()

	engine, err := New(context.Background())
	if err != nil {
		t.Fatalf("create Unicorn engine: %v", err)
	}

	return engine
}
