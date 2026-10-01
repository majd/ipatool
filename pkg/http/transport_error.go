package http

// TransportError identifies Send failures while sending a request or reading
// its response, so callers can distinguish them from signing, encoding and
// storage failures before deciding whether to retry.
type TransportError struct {
	Err error
	// RetryAfter retains Apple's backoff even when reading the response fails.
	RetryAfter string
}

func (e *TransportError) Error() string {
	return e.Err.Error()
}

func (e *TransportError) Unwrap() error {
	return e.Err
}
