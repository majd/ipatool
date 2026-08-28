package http

import (
	"encoding/base64"
	"errors"
	"io"
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

			It("signs the exact serialized request body", func() {
				signature := []byte("test-signature")
				signer := &recordingActionSigner{signature: signature}

				mockHandler = func(w http.ResponseWriter, r *http.Request) {
					defer GinkgoRecover()

					body, err := io.ReadAll(r.Body)
					Expect(err).ToNot(HaveOccurred())
					Expect(body).To(Equal(signer.data))
					Expect(r.Header.Get(HeaderAppleActionSignature)).To(Equal(base64.StdEncoding.EncodeToString(signature)))
					_, err = w.Write([]byte("{\"foo\":\"bar\"}"))
					Expect(err).ToNot(HaveOccurred())
				}

				sut := NewClient[jsonResult](Args{CookieJar: mockCookieJar})
				_, err := sut.Send(Request{
					URL:            srv.URL,
					Method:         MethodPOST,
					ResponseFormat: ResponseFormatJSON,
					ActionSigner:   signer,
					Payload: &XMLPayload{Content: map[string]interface{}{
						"appleId": "test@example.com",
						"attempt": "1",
					}},
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(signer.calls).To(Equal(1))
			})

			It("preserves an authentication pod redirect", func() {
				podURL := srv.URL + "/pod"
				mockHandler = func(w http.ResponseWriter, r *http.Request) {
					defer GinkgoRecover()
					Expect(r.URL.Path).To(Equal(appStoreAuthPath))
					w.Header().Set("Location", podURL)
					w.WriteHeader(http.StatusFound)
				}

				sut := NewClient[xmlResult](Args{CookieJar: mockCookieJar})
				result, err := sut.Send(Request{
					URL:            srv.URL + appStoreAuthPath,
					Method:         MethodPOST,
					ResponseFormat: ResponseFormatXML,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(result.StatusCode).To(Equal(http.StatusFound))
				Expect(result.Headers).To(HaveKeyWithValue("Location", podURL))
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

			It("decodes XML response wrapped in an Apple Document envelope", func() {
				mockHandler = func(w http.ResponseWriter, _r *http.Request) {
					w.Header().Add("Content-Type", "application/xml")
					_, err := w.Write([]byte("<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"no\"?>\n<Document xmlns=\"http://www.apple.com/itms/\"><key>foo</key><string>bar</string></Document>"))
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

			It("decodes XML response wrapped in Document/Protocol/plist with nested dict", func() {
				mockHandler = func(w http.ResponseWriter, _r *http.Request) {
					w.Header().Add("Content-Type", "application/xml")
					_, err := w.Write([]byte("<?xml version=\"1.0\" encoding=\"UTF-8\" standalone=\"no\"?>\n<Document xmlns=\"http://www.apple.com/itms/\"><Protocol><plist version=\"1.0\"><dict><key>nested</key><dict><key>a</key><string>b</string></dict><key>foo</key><string>bar</string></dict></plist></Protocol></Document>"))
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

			It("returns a readable error when Apple responds with an HTML page instead of a plist", func() {
				mockHandler = func(w http.ResponseWriter, _r *http.Request) {
					w.Header().Add("Content-Type", "text/html")
					w.WriteHeader(http.StatusServiceUnavailable)
					_, err := w.Write([]byte("<html><body><h1>Service Unavailable</h1></body></html>"))
					Expect(err).ToNot(HaveOccurred())
				}

				sut := NewClient[xmlResult](Args{
					CookieJar: mockCookieJar,
				})
				_, err := sut.Send(Request{
					URL:            srv.URL,
					Method:         MethodPOST,
					ResponseFormat: ResponseFormatXML,
				})

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).ToNot(ContainSubstring("hex digit"))
				Expect(err.Error()).To(ContainSubstring("Service Unavailable"))
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

	It("preserves an XML redirect response and its location header", func() {
		recorder := httptest.NewRecorder()
		recorder.Header().Set("Location", "https://p7-buy.itunes.apple.com/authenticate")
		recorder.WriteHeader(http.StatusFound)

		sut := &client[xmlResult]{}
		res, err := sut.handleXMLResponse(recorder.Result())

		Expect(err).ToNot(HaveOccurred())
		Expect(res.StatusCode).To(Equal(http.StatusFound))
		Expect(res.Headers).To(HaveKeyWithValue("Location", "https://p7-buy.itunes.apple.com/authenticate"))
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

	When("action signing fails", func() {
		It("returns a wrapped error without sending the request", func() {
			signer := &recordingActionSigner{err: errors.New("signing error")}
			sut := NewClient[xmlResult](Args{CookieJar: mockCookieJar})

			_, err := sut.Send(Request{
				URL:            srv.URL,
				Method:         MethodPOST,
				ResponseFormat: ResponseFormatXML,
				ActionSigner:   signer,
				Payload:        &XMLPayload{Content: map[string]interface{}{"attempt": "1"}},
			})

			Expect(err).To(MatchError("failed to sign Apple action: signing error"))
			Expect(signer.calls).To(Equal(1))
		})
	})
})

type recordingActionSigner struct {
	data      []byte
	signature []byte
	err       error
	calls     int
}

func (s *recordingActionSigner) Sign(data []byte) ([]byte, error) {
	s.calls++

	s.data = append([]byte(nil), data...)

	return s.signature, s.err
}
