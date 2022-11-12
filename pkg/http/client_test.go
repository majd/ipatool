package http

import (
	"context"
	"fmt"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"math/rand"
	"net/http"
)

type JSONResult struct {
	Foo string `json:"foo"`
}

type XMLResult struct {
	Foo string `plist:"foo"`
}

var _ = Describe("Client", Ordered, func() {
	var (
		port   int
		ctx    context.Context
		cancel context.CancelFunc
	)

	BeforeAll(func() {
		port = rand.Intn(59_999-50_000) + 50_000
		ctx, cancel = context.WithCancel(context.Background())

		http.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
			res := []byte("{\"foo\":\"bar\"}")
			w.Header().Add("Content-Type", "application/json")
			_, err := w.Write(res)
			Expect(err).ToNot(HaveOccurred())
		})

		http.HandleFunc("/xml", func(w http.ResponseWriter, r *http.Request) {
			res := []byte("<dict><key>foo</key><string>bar</string></dict>")
			w.Header().Add("Content-Type", "application/xml")
			_, err := w.Write(res)
			Expect(err).ToNot(HaveOccurred())
		})

		http.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Content-Type", "application/random-type")
		})

		go func() {
			err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
			Expect(err).ToNot(HaveOccurred())
			<-ctx.Done()
		}()
	})

	AfterAll(func() {
		cancel()
	})

	It("decodes JSON response", func() {
		sut := NewClient[JSONResult](nil)
		res, err := sut.Send(Request{
			URL:    fmt.Sprintf("http://localhost:%d/json", port),
			Method: MethodGET,
			Headers: map[string]string{
				"foo": "bar",
			},
			Payload: &URLPayload{
				Content: map[string]interface{}{
					"data": "test",
				},
			},
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(res.Data.Foo).To(Equal("bar"))
	})

	It("decodes XML response", func() {
		sut := NewClient[XMLResult](nil)
		res, err := sut.Send(Request{
			URL:    fmt.Sprintf("http://localhost:%d/xml", port),
			Method: MethodPOST,
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(res.Data.Foo).To(Equal("bar"))
	})

	It("returns error when content type is not supported", func() {
		sut := NewClient[XMLResult](nil)
		_, err := sut.Send(Request{
			URL:    fmt.Sprintf("http://localhost:%d/error", port),
			Method: MethodPOST,
		})

		Expect(err).To(MatchError(ContainSubstring("unsupported response body content type: application/random-type")))
	})

	It("returns error when failing to read payload", func() {
		sut := NewClient[XMLResult](nil)
		_, err := sut.Send(Request{
			URL:    fmt.Sprintf("http://localhost:%d/error", port),
			Method: MethodPOST,
			Payload: &URLPayload{
				Content: map[string]interface{}{
					"data": func() {},
				},
			},
		})

		Expect(err).To(MatchError(ContainSubstring("failed to read payload data")))
	})
})
