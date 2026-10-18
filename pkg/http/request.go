package http

import (
	"context"
	"time"
)

type Request struct {
	// Timeout applies to transport I/O after runtime asset preparation.
	Timeout        time.Duration
	Context        context.Context
	NoRedirects    bool
	Method         string
	URL            string
	Headers        map[string]string
	Payload        Payload
	ActionSigner   ActionSigner
	ResponseFormat ResponseFormat
}

type ActionSigner interface {
	Sign(data []byte) ([]byte, error)
}
