package http

type Result[R interface{}] struct {
	StatusCode int
	Headers    map[string]string
	Data       R
}
