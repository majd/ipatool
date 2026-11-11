package unicorn

import "fmt"

const (
	ucCtlTCGBufferSize = 13
	ucCtlIOWrite       = 1
	ucCtlArgumentCount = 1
	tcgBufferSize      = 4 << 20
)

func configureEngine(engine *Engine) error {
	// Bound native JIT memory on every host, including memory-limited Linux
	// containers. Unicorn flushes translated blocks when the buffer fills.
	control := uint32(ucCtlTCGBufferSize |
		ucCtlArgumentCount<<26 |
		ucCtlIOWrite<<30)
	if err := engine.err(engine.api.ctl(engine.handle, control, tcgBufferSize)); err != nil {
		return fmt.Errorf("configure Unicorn translation buffer: %w", err)
	}

	return nil
}
