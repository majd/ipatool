package http

import (
	"errors"
	"strings"
)

var (
	ErrHeaderNotFound = errors.New("header not found")
)

type Result[R interface{}] struct {
	StatusCode int
	Headers    map[string]string
	Data       R
}

func (c *Result[R]) GetHeader(key string) (string, error) {
	key = strings.ToLower(key)
	for k, v := range c.Headers {
		if strings.ToLower(k) == key {
			return v, nil
		}
	}

	return "", ErrHeaderNotFound
}
