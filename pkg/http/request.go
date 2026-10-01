package http

import "context"

type Request struct {
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
