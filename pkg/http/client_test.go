package http

import (
	"bytes"
	"context"
	"errors"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("Typed response client", func() {
	type result struct {
		Foo string `json:"foo" plist:"foo"`
	}
	response := func(body string) *gohttp.Response {
		recorder := httptest.NewRecorder()
		_, err := recorder.WriteString(body)
		Expect(err).NotTo(HaveOccurred())

		return recorder.Result()
	}
	It("requires an executor instead of falling back to direct networking", func() {
		client := NewClient[result](Args{})
		_, err := client.Send(Request{URL: "https://example.invalid", Method: MethodGET})
		Expect(err).To(MatchError("request executor is not configured"))
		req, err := client.NewRequest(MethodGET, "https://example.invalid", nil)
		Expect(err).NotTo(HaveOccurred())
		_, err = client.Do(req)
		Expect(err).To(MatchError("request executor is not configured"))
	})
	DescribeTable("decodes framework responses", func(format ResponseFormat, body string) {
		client := NewClient[result](Args{Executor: testExecutor{send: func(Request, []byte) (*gohttp.Response, error) { return response(body), nil }}})
		got, err := client.Send(Request{ResponseFormat: format})
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Data.Foo).To(Equal("bar"))
	},
		Entry("JSON", ResponseFormatJSON, `{"foo":"bar"}`),
		Entry("plist", ResponseFormatXML, `<plist><dict><key>foo</key><string>bar</string></dict></plist>`),
		Entry("Document", ResponseFormatXML, `<Document><dict><key>foo</key><string>bar</string></dict></Document>`),
		Entry("nested Document", ResponseFormatXML, `<Document><Protocol><plist><dict><key>foo</key><string>bar</string></dict></plist></Protocol></Document>`),
	)
	It("preserves raw bytes and response headers", func() {
		client := NewClient[[]byte](Args{Executor: testExecutor{send: func(Request, []byte) (*gohttp.Response, error) {
			res := response(string([]byte{0, 1, 255}))
			res.Header.Set("X-Test", "fixture")

			return res, nil
		}}})
		got, err := client.Send(Request{ResponseFormat: ResponseFormatRaw})
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Data).To(Equal([]byte{0, 1, 255}))
		Expect(got.GetHeader("x-test")).To(Equal("fixture"))
	})
	It("copies raw request data and delegates context, headers, and redirect policy", func() {
		payload := &RawPayload{Content: []byte{0, 1, 255}}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		client := NewClient[[]byte](Args{Executor: testExecutor{send: func(req Request, body []byte) (*gohttp.Response, error) {
			Expect(req.Context).To(BeIdenticalTo(ctx))
			Expect(req.NoRedirects).To(BeTrue())
			Expect(req.Headers).To(HaveKeyWithValue("X-Token", "fixture"))
			Expect(body).To(Equal(payload.Content))
			body[0] = 99

			return response("ok"), nil
		}}})
		_, err := client.Send(Request{Context: ctx, NoRedirects: true, Headers: map[string]string{"X-Token": "fixture"}, Payload: payload, ResponseFormat: ResponseFormatRaw})
		Expect(err).NotTo(HaveOccurred())
		Expect(payload.Content).To(Equal([]byte{0, 1, 255}))
	})
	DescribeTable("forwards configured timeouts", func(args Args, want time.Duration) {
		args.Executor = testExecutor{send: func(req Request, _ []byte) (*gohttp.Response, error) {
			Expect(req.Timeout).To(Equal(want))

			return response("ok"), nil
		}}
		_, err := NewClient[[]byte](args).Send(Request{ResponseFormat: ResponseFormatRaw})
		Expect(err).NotTo(HaveOccurred())
	}, Entry("authentication default", Args{Authentication: true}, 30*time.Second), Entry("override", Args{Authentication: true, Timeout: 5 * time.Second}, 5*time.Second), Entry("bag", Args{Timeout: DefaultAuthenticationTimeout}, 30*time.Second), Entry("unlimited transfer", Args{}, time.Duration(0)))
	It("saves cookies and preserves local storage failures", func() {
		jar := NewMockCookieJar(gomock.NewController(GinkgoT()))
		jar.EXPECT().Save().Return(context.DeadlineExceeded)
		client := NewClient[result](Args{CookieJar: jar, Executor: testExecutor{send: func(Request, []byte) (*gohttp.Response, error) { return response("<dict/>"), nil }}})
		_, err := client.Send(Request{ResponseFormat: ResponseFormatXML})
		Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
		var transport *TransportError
		Expect(errors.As(err, &transport)).To(BeFalse())
	})
	It("closes response bodies after decoding", func() {
		body := &trackedResponseBody{Reader: bytes.NewReader([]byte(`{"foo":"bar"}`))}
		client := NewClient[result](Args{Executor: testExecutor{send: func(Request, []byte) (*gohttp.Response, error) {
			return &gohttp.Response{StatusCode: 200, Header: make(gohttp.Header), Body: body}, nil
		}}})
		_, err := client.Send(Request{ResponseFormat: ResponseFormatJSON})
		Expect(err).NotTo(HaveOccurred())
		Expect(body.closed).To(BeTrue())
	})
	It("rejects absent executor responses", func() {
		client := NewClient[result](Args{Executor: testExecutor{send: func(Request, []byte) (*gohttp.Response, error) { return nil, nil }}})
		_, err := client.Send(Request{})
		Expect(err).To(MatchError("request returned no HTTP response"))
	})
	It("rejects incompatible raw result types and unsupported formats", func() {
		client := NewClient[result](Args{Executor: testExecutor{send: func(Request, []byte) (*gohttp.Response, error) { return response("body"), nil }}})
		_, err := client.Send(Request{ResponseFormat: ResponseFormatRaw})
		Expect(err).To(HaveOccurred())
		_, err = client.Send(Request{})
		Expect(err).To(MatchError("content type is not supported ()"))
	})
})

type trackedResponseBody struct {
	io.Reader
	closed bool
}

func (b *trackedResponseBody) Close() error {
	b.closed = true

	return nil
}
