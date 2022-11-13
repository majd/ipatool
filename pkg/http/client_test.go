package http

import (
	"context"
	"errors"
	"fmt"
	"github.com/golang/mock/gomock"
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
		port          int
		ctx           context.Context
		cancel        context.CancelFunc
		ctrl          *gomock.Controller
		mockCookieJar *MockCookieJar
	)

	BeforeAll(func() {
		ctrl = gomock.NewController(GinkgoT())
		port = rand.Intn(59_999-50_000) + 50_000
		ctx, cancel = context.WithCancel(context.Background())
		mockCookieJar = NewMockCookieJar(ctrl)

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

	BeforeEach(func() {
		mockCookieJar.EXPECT().
			Cookies(gomock.Any()).
			Return(nil).
			MaxTimes(1)
	})

	When("payload decodes successfully", func() {
		When("cookie jar fails to save", func() {
			var testErr = errors.New("test")

			BeforeEach(func() {
				mockCookieJar.EXPECT().
					Save().
					Return(testErr)
			})

			It("returns error", func() {
				sut := NewClient[JSONResult](&Args{
					CookieJar: mockCookieJar,
				})
				_, err := sut.Send(Request{
					URL:    fmt.Sprintf("http://localhost:%d/json", port),
					Method: MethodGET,
				})

				Expect(err).To(MatchError(ContainSubstring(testErr.Error())))
				Expect(err).To(MatchError(ContainSubstring("failed to save cookies")))
			})
		})

		When("cookie jar saves new cookies", func() {
			BeforeEach(func() {
				mockCookieJar.EXPECT().
					Save().
					Return(nil)
			})

			It("decodes JSON response", func() {
				sut := NewClient[JSONResult](&Args{
					CookieJar: mockCookieJar,
				})
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
				sut := NewClient[XMLResult](&Args{
					CookieJar: mockCookieJar,
				})
				res, err := sut.Send(Request{
					URL:    fmt.Sprintf("http://localhost:%d/xml", port),
					Method: MethodPOST,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(res.Data.Foo).To(Equal("bar"))
			})

			It("returns error when content type is not supported", func() {
				sut := NewClient[XMLResult](&Args{
					CookieJar: mockCookieJar,
				})
				_, err := sut.Send(Request{
					URL:    fmt.Sprintf("http://localhost:%d/error", port),
					Method: MethodPOST,
				})

				Expect(err).To(MatchError(ContainSubstring("unsupported response body content type: application/random-type")))
			})
		})
	})

	When("payload fails to decode", func() {
		It("returns error", func() {
			sut := NewClient[XMLResult](&Args{
				CookieJar: mockCookieJar,
			})
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
})
