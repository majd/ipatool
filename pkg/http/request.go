package http

type Request struct {
	Method         string
	URL            string
	Headers        map[string]string
	Payload        Payload
	ResponseFormat ResponseFormat
}
