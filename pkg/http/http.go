package http

//go:generate mockgen -source=cookiejar.go -destination=cookiejar_mock.go -package=http
//go:generate mockgen -source=client.go -destination=client_mock.go -package=http
