package emulator

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/store/profile"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"howett.net/plist"
)

var _ = Describe("Purchase terms presentation", func() {
	DescribeTable("returns the original terms response to the caller", func(compressed bool) {
		ctx := context.Background()
		bundle, err := assets.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		prepared, err := profile.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		guest, err := open(ctx, bundle.AppleMediaServices, prepared, true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(guest.Close)
		body := []byte(`<plist version="1.0"><dict><key>pings</key><array></array><key>jingleDocType</key><string>buyProductFailure</string><key>jingleAction</key><string>buyProduct</string><key>action</key><dict><key>kind</key><string>Goto</string><key>url</key><string>https://p45-buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/com.apple.jingle.app.finance.DirectAction/termsPage</string><key>target</key><string>main</string></dict></dict></plist>`)
		headers := http.Header{"Content-Type": {"text/xml; charset=UTF-8"}}
		var expected map[string]any
		_, err = plist.Unmarshal(body, &expected)
		Expect(err).NotTo(HaveOccurred())
		if compressed {
			var buffer bytes.Buffer
			writer := gzip.NewWriter(&buffer)
			_, err = writer.Write(body)
			Expect(err).NotTo(HaveOccurred())
			Expect(writer.Close()).To(Succeed())
			body = buffer.Bytes()
			headers.Set("Content-Encoding", "gzip")
		}
		calls := 0
		response, err := guest.Execute(Request{
			Operation: "purchase", URL: "https://p45-buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/buyProduct", Method: http.MethodPost,
			HardwareID: []byte{0, 1, 2, 3, 4, 5}, Bag: map[string]any{"buyProduct": "https://buy.itunes.apple.com/request"},
			Parameters: map[string]any{"price": "0", "productType": "C", "salableAdamId": "1234", "pricingParameters": "STDQ"},
			Transport: func(request *http.Request) (*http.Response, error) {
				calls++

				return &http.Response{StatusCode: http.StatusOK, Header: headers.Clone(), Body: io.NopCloser(bytes.NewReader(body))}, nil
			},
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(response.Body.Close)
		actual, err := io.ReadAll(response.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(calls).To(Equal(1))
		var decoded map[string]any
		_, err = plist.Unmarshal(actual, &decoded)
		Expect(err).NotTo(HaveOccurred())
		Expect(decoded).To(Equal(expected))
		Expect(response.Header.Get("Content-Encoding")).To(BeEmpty())
	}, Entry("plain XML", false), Entry("gzip, as in issue 665", true))
})
