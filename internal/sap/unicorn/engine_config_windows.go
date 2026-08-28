package unicorn

import "fmt"

const (
	ucCtlTCGBufferSize = 13
	ucCtlIOWrite       = 1
	ucCtlArgumentCount = 1
	tcgBufferSize      = 64 << 20
)

func configureEngine(engine *Engine) error {
	// The VirtualAlloc compatibility hook commits this buffer eagerly, so keep
	// its upper bound modest instead of using Unicorn's much larger default.
	control := uint32(ucCtlTCGBufferSize |
		ucCtlArgumentCount<<26 |
		ucCtlIOWrite<<30)
	if err := engine.err(engine.api.ctl(engine.handle, control, tcgBufferSize)); err != nil {
		return fmt.Errorf("configure Unicorn translation buffer: %w", err)
	}
	return nil
}
