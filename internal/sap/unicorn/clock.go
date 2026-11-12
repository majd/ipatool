package unicorn

import (
	"context"
	"fmt"
	"sync"
)

// Unicorn 2.1.4's machine_initialize calls init_get_clock for every engine.
// On Unix this briefly switches the process-wide use_rt_clock from monotonic
// time to wall time, which can make another engine's timeout expire immediately.
// Initialize only while no engines are executing; initialized engines can still
// execute concurrently, including nested calls from guest callbacks.
var engineClock clockState

type clockState struct {
	mu     sync.Mutex
	active int
	idle   chan struct{}
}

func (c *clockState) initialize(ctx context.Context, initialize func() error) error {
	for {
		c.mu.Lock()
		if err := ctx.Err(); err != nil {
			c.mu.Unlock()

			return fmt.Errorf("wait for Unicorn clock: %w", err)
		}

		if c.active == 0 {
			err := initialize()
			c.mu.Unlock()

			return err
		}

		if c.idle == nil {
			c.idle = make(chan struct{})
		}

		idle := c.idle
		c.mu.Unlock()

		select {
		case <-idle:
		case <-ctx.Done():
			return fmt.Errorf("wait for Unicorn clock: %w", ctx.Err())
		}
	}
}

func (c *clockState) beginEmulation() func() {
	c.mu.Lock()
	c.active++
	c.mu.Unlock()

	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()

		c.active--
		if c.active == 0 && c.idle != nil {
			close(c.idle)
			c.idle = nil
		}
	}
}
