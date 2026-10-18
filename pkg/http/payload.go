package http

// Payload describes input for the request executor. Property lists are encoded
// by the Store framework; pre-encoded binary requests are supplied as raw data.
type Payload interface{ isPayload() }

type XMLPayload struct{ Content map[string]interface{} }
type RawPayload struct{ Content []byte }

func (*XMLPayload) isPayload() {}
func (*RawPayload) isPayload() {}
