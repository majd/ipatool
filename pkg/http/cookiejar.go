package http

import "net/http"

type CookieJar interface {
	http.CookieJar

	Save() error
}
