package http

import "net/http"

//go:generate mockgen -source=cookiejar.go -destination=cookiejar_mock.go -package=http
type CookieJar interface {
	http.CookieJar

	Save() error
}
