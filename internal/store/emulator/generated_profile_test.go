package emulator

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/store/profile"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Generated Store profile", func() {
	DescribeTable("encodes and handles responses through the pinned frameworks", func(operation string, savedAccount bool) {
		ctx := context.Background()
		bundle, err := assets.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		prepared, err := profile.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		guest, err := open(ctx, bundle.AppleMediaServices, prepared, true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(guest.Close)
		calls := 0
		input := Request{Operation: operation, URL: "https://buy.itunes.apple.com/request", Method: http.MethodPost, HardwareID: []byte{0, 1, 2, 3, 4, 5}, Headers: http.Header{}, Parameters: map[string]any{"salableAdamId": "1234", "pricingParameters": "STDQ", "price": "0", "productType": "C", "appleId": "test@example.com", "password": "test"}, Bag: map[string]any{"buyProduct": "https://buy.itunes.apple.com/request"}, Sign: func([]byte) ([]byte, error) { return []byte{1, 2, 3}, nil }, Transport: func(request *http.Request) (*http.Response, error) {
			calls++
			Expect(request.URL.Scheme).To(Equal("https"))
			Expect(request.URL.Host).To(Equal("buy.itunes.apple.com"))
			Expect(request.Method).To(Equal(http.MethodPost))

			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/x-apple-plist"}}, Body: io.NopCloser(strings.NewReader(`<?xml version="1.0"?><plist version="1.0"><dict><key>status</key><integer>0</integer></dict></plist>`))}, nil
		}}
		if savedAccount {
			input.Headers.Set("X-Dsid", "123456")
			input.Headers.Set("X-Token", "test-token")
			input.Headers.Set("X-Apple-Store-Front", "143441-1,29")
		}

		response, err := guest.Execute(input)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(response.Body.Close)
		Expect(calls).To(Equal(1))
		Expect(response.StatusCode).To(Equal(http.StatusOK))
	}, Entry("authentication", "authenticate", false), Entry("authentication with a saved account", "authenticate", true), Entry("free purchases", "purchase", true), Entry("downloads", "download", true), Entry("search", "search", true), Entry("lookup", "lookup", false))
})

var _ = Describe("Generated profile response branches", func() {
	DescribeTable("preserves catalog, transfer, and finance responses", func(operation, method, contentType, body string, status int, stream bool) {
		ctx := context.Background()
		bundle, err := assets.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		prepared, err := profile.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		guest, err := open(ctx, bundle.AppleMediaServices, prepared, true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(guest.Close)

		input := Request{
			Operation: operation, URL: "https://buy.itunes.apple.com/request", Method: method,
			HardwareID: []byte{0, 1, 2, 3, 4, 5}, Headers: http.Header{}, Stream: stream,
			Bag: map[string]any{"buyProduct": "https://buy.itunes.apple.com/request"},
			Transport: func(request *http.Request) (*http.Response, error) {
				Expect(request.Method).To(Equal(method))

				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			},
		}
		if operation == "authenticate" || operation == "purchase" {
			input.Parameters = map[string]any{"appleId": "test@example.com", "password": "test", "salableAdamId": "1234", "pricingParameters": "STDQ", "price": "0", "productType": "C"}
			input.Sign = func([]byte) ([]byte, error) { return []byte{1, 2, 3}, nil }
		}

		response, err := guest.Execute(input)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(response.Body.Close)
		Expect(response.StatusCode).To(Equal(status))
		actual, err := io.ReadAll(response.Body)
		Expect(err).NotTo(HaveOccurred())
		if stream {
			Expect(actual).To(Equal([]byte(body)))
		} else {
			Expect(string(actual)).To(ContainSubstring("fixture-value"))
		}
	},
		Entry("catalog GET", "search", http.MethodGet, "application/json", `{"resultCount":1,"results":[{"bundleId":"fixture-value"}]}`, http.StatusOK, false),
		Entry("version lookup GET", "lookup", http.MethodGet, "application/json", `{"results":{"fixture-value":{"offers":[]}}}`, http.StatusOK, false),
		Entry("streamed package range", "download", http.MethodGet, "application/octet-stream", "fixture-value\x00\xff", http.StatusPartialContent, true),
		Entry("authentication failure", "authenticate", http.MethodPost, "application/x-apple-plist", `<plist version="1.0"><dict><key>failureType</key><string>2034</string><key>customerMessage</key><string>fixture-value</string></dict></plist>`, http.StatusOK, false),
		Entry("purchase failure", "purchase", http.MethodPost, "application/x-apple-plist", `<plist version="1.0"><dict><key>failureType</key><string>9610</string><key>customerMessage</key><string>fixture-value</string></dict></plist>`, http.StatusOK, false),
	)
})

var _ = Describe("Missing profile snapshots", func() {
	It("rejects an omitted page even though its neighbors share a grouped mapping", func() {
		ctx := context.Background()
		bundle, err := assets.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		prepared, err := profile.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		var metadata Profile
		Expect(json.Unmarshal(prepared.Metadata, &metadata)).To(Succeed())
		// Bootstrap heap pages are outside both Apple images and the guest heap.
		// Drop an interior page so its neighbors still map the omitted address.
		var missing uint64
		for i := 1; i+1 < len(metadata.Pages); i++ {
			address := metadata.Pages[i].Address
			if address > 1<<40 && address < heapBase && metadata.Pages[i-1].Address+4096 == address && metadata.Pages[i+1].Address == address+4096 {
				missing = address
				metadata.Pages = slices.Delete(metadata.Pages, i, i+1)

				break
			}
		}

		Expect(missing).NotTo(BeZero())
		prepared.Metadata, err = json.Marshal(metadata)
		Expect(err).NotTo(HaveOccurred())
		guest, err := open(ctx, bundle.AppleMediaServices, prepared, true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(guest.Close)
		Expect(guest.pages[missing]).To(BeTrue())
		_, err = guest.engine.MemRead(missing, 8)
		Expect(err).To(MatchError(ContainSubstring("missing Store profile page")))
	})
})
