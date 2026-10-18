package http

import (
	"bytes"
	"context"
	"errors"
	"io"
	gohttp "net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

type testExecutor struct {
	send func(Request, []byte) (*gohttp.Response, error)
	do   func(*gohttp.Request) (*gohttp.Response, error)
}

func (e testExecutor) Send(r Request, b []byte) (*gohttp.Response, error) { return e.send(r, b) }
func (e testExecutor) Do(r *gohttp.Request) (*gohttp.Response, error)     { return e.do(r) }

var _ = Describe("Framework executor compatibility", func() {
	It("passes configured transport timeouts to the framework", func() {
		client := NewClient[any](Args{Timeout: 25 * time.Millisecond, Executor: testExecutor{send: func(request Request, _ []byte) (*gohttp.Response, error) {
			Expect(request.Timeout).To(Equal(25 * time.Millisecond))

			return nil, context.DeadlineExceeded
		}}})
		_, err := client.Send(Request{URL: "https://itunes.apple.com/fixture"})
		Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
	})
	It("delegates signing and preserves typed plist results and headers", func() {
		jar := NewMockCookieJar(gomock.NewController(GinkgoT()))
		jar.EXPECT().Save().Return(nil)
		signer := &executorSigner{}
		called := false
		client := NewClient[struct {
			Token string `plist:"token"`
		}](Args{CookieJar: jar, Executor: testExecutor{send: func(request Request, body []byte) (*gohttp.Response, error) {
			called = true
			Expect(request.ActionSigner).To(BeIdenticalTo(signer))
			Expect(body).To(BeNil())
			Expect(request.Payload.(*XMLPayload).Content).To(HaveKeyWithValue("password", "fixture"))

			return &gohttp.Response{StatusCode: 200, Header: gohttp.Header{"Pod": []string{"35"}}, Body: io.NopCloser(bytes.NewBufferString(`<plist><dict><key>token</key><string>fixture-token</string></dict></plist>`))}, nil
		}}})
		result, err := client.Send(Request{Method: "POST", URL: "https://buy.itunes.apple.com/fixture", ResponseFormat: ResponseFormatXML, Payload: &XMLPayload{Content: map[string]any{"password": "fixture"}}, ActionSigner: signer})
		Expect(err).NotTo(HaveOccurred())
		Expect(called).To(BeTrue())
		Expect(signer.called).To(BeFalse())
		Expect(result.Data.Token).To(Equal("fixture-token"))
		Expect(result.GetHeader("pod")).To(Equal("35"))
	})
	It("preserves cancellation through the transport error wrapper", func() {
		client := NewClient[any](Args{Executor: testExecutor{send: func(Request, []byte) (*gohttp.Response, error) { return nil, context.Canceled }}})
		_, err := client.Send(Request{URL: "https://itunes.apple.com/fixture"})
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		var transport *TransportError
		Expect(errors.As(err, &transport)).To(BeTrue())
	})
	It("returns the streaming response without reading its body", func() {
		body := &unreadBody{}
		client := NewClient[any](Args{Executor: testExecutor{do: func(request *gohttp.Request) (*gohttp.Response, error) {
			Expect(request.Header.Get("Range")).To(Equal("bytes=123-"))

			return &gohttp.Response{StatusCode: 206, Body: body}, nil
		}}})
		request, err := client.NewRequest("GET", "https://example.mzstatic.com/file", nil)
		Expect(err).NotTo(HaveOccurred())
		request.Header.Set("Range", "bytes=123-")
		response, err := client.Do(request)
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(206))
		Expect(body.read).To(BeFalse())
		Expect(response.Body.Close()).To(Succeed())
		Expect(body.closed).To(BeTrue())
	})
})

type executorSigner struct{ called bool }

func (s *executorSigner) Sign([]byte) ([]byte, error) {
	s.called = true

	return nil, errors.New("signing must occur after native encoding")
}

type unreadBody struct{ read, closed bool }

func (b *unreadBody) Read([]byte) (int, error) {
	b.read = true

	return 0, io.EOF
}
func (b *unreadBody) Close() error {
	b.closed = true

	return nil
}
