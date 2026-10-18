package http

import gohttp "net/http"

// Executor delegates request encoding and execution to a Store runtime. The
// generic client retains the existing response types and cookie persistence.
type Executor interface {
	Send(Request, []byte) (*gohttp.Response, error)
	Do(*gohttp.Request) (*gohttp.Response, error)
}
