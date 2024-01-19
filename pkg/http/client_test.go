package http

import (
	"errors"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("Client", Ordered, func() {
	type jsonResult struct {
		Foo string `json:"foo"`
	}

	type xmlResult struct {
		Foo string `plist:"foo"`
	}

	var (
		ctrl          *gomock.Controller
		srv           *httptest.Server
		mockHandler   func(w http.ResponseWriter, r *http.Request)
		mockCookieJar *MockCookieJar
	)

	BeforeAll(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockCookieJar = NewMockCookieJar(ctrl)
		mockHandler = func(w http.ResponseWriter, r *http.Request) {}
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mockHandler(w, r)
		}))
	})

	BeforeEach(func() {
		mockCookieJar.EXPECT().
			Cookies(gomock.Any()).
			Return(nil).
			MaxTimes(1)
	})

	It("returns request", func() {
		sut := NewClient[xmlResult](Args{})

		req, err := sut.NewRequest("GET", srv.URL, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(req).ToNot(BeNil())
	})

	It("returns response", func() {
		mockHandler = func(_w http.ResponseWriter, r *http.Request) {
			defer GinkgoRecover()
			Expect(r.Header.Get("User-Agent")).To(Equal(DefaultUserAgent))
		}

		sut := NewClient[xmlResult](Args{})

		req, err := sut.NewRequest("GET", srv.URL, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(req).ToNot(BeNil())

		res, err := sut.Do(req)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(BeNil())
	})

	When("payload decodes successfully", func() {
		When("cookie jar fails to save", func() {
			BeforeEach(func() {
				mockCookieJar.EXPECT().
					Save().
					Return(errors.New(""))
			})

			It("returns error", func() {
				sut := NewClient[jsonResult](Args{
					CookieJar: mockCookieJar,
				})
				_, err := sut.Send(Request{
					URL:    srv.URL,
					Method: MethodGET,
				})

				Expect(err).To(HaveOccurred())
			})
		})

		When("cookie jar saves new cookies", func() {
			BeforeEach(func() {
				mockCookieJar.EXPECT().
					Save().
					Return(nil)
			})

			It("decodes JSON response", func() {
				mockHandler = func(w http.ResponseWriter, _r *http.Request) {
					w.Header().Add("Content-Type", "application/json")
					_, err := w.Write([]byte("{\"foo\":\"bar\"}"))
					Expect(err).ToNot(HaveOccurred())
				}

				sut := NewClient[jsonResult](Args{
					CookieJar: mockCookieJar,
				})
				res, err := sut.Send(Request{
					URL:            srv.URL,
					Method:         MethodGET,
					ResponseFormat: ResponseFormatJSON,
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
				mockHandler = func(w http.ResponseWriter, _r *http.Request) {
					w.Header().Add("Content-Type", "application/xml")
					_, err := w.Write([]byte("<dict><key>foo</key><string>bar</string></dict>"))
					Expect(err).ToNot(HaveOccurred())
				}

				sut := NewClient[xmlResult](Args{
					CookieJar: mockCookieJar,
				})
				res, err := sut.Send(Request{
					URL:            srv.URL,
					Method:         MethodPOST,
					ResponseFormat: ResponseFormatXML,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(res.Data.Foo).To(Equal("bar"))
			})

			It("returns error when content type is not supported", func() {
				mockHandler = func(w http.ResponseWriter, _r *http.Request) {
					w.Header().Add("Content-Type", "application/xyz")
				}

				sut := NewClient[xmlResult](Args{
					CookieJar: mockCookieJar,
				})
				_, err := sut.Send(Request{
					URL:            srv.URL,
					Method:         MethodPOST,
					ResponseFormat: "random",
				})

				Expect(err).To(HaveOccurred())
			})
		})
	})

	When("payload fails to decode", func() {
		It("returns error", func() {
			sut := NewClient[xmlResult](Args{
				CookieJar: mockCookieJar,
			})
			_, err := sut.Send(Request{
				URL:            srv.URL,
				Method:         MethodPOST,
				ResponseFormat: ResponseFormatXML,
				Payload: &URLPayload{
					Content: map[string]interface{}{
						"data": func() {},
					},
				},
			})

			Expect(err).To(HaveOccurred())
		})
	})
})
