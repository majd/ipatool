package emulator

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/store/profile"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Temporary purchase diagnostics", func() {
	It("captures the response before the reported Goto crash", func() {
		path := filepath.Join(GinkgoT().TempDir(), "purchase.jsonl")
		GinkgoT().Setenv("IPATOOL_PURCHASE_TRACE", path)
		ctx := context.Background()
		bundle, err := assets.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		prepared, err := profile.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		guest, err := open(ctx, bundle.AppleMediaServices, prepared, true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(guest.Close)
		body := `<plist version="1.0"><dict><key>action</key><dict><key>kind</key><string>Goto</string><key>url</key><string>https://apps.apple.com/account</string></dict></dict></plist>`
		headers := http.Header{}
		headers.Set("X-Apple-Store-Front", "143462-1,34")
		headers.Set("X-Dsid", "123456")
		headers.Set("X-Token", "request-secret")
		_, err = guest.Execute(Request{
			Operation: "purchase", URL: "https://buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/buyProduct", Method: http.MethodPost,
			HardwareID: []byte{0, 1, 2, 3, 4, 5}, Headers: headers,
			Parameters: map[string]any{"price": "0", "productType": "C", "salableAdamId": "1234", "pricingParameters": "STDQ"},
			Bag:        map[string]any{"buyProduct": "https://buy.itunes.apple.com/request"},
			Transport: func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/x-apple-plist"}, "Set-Cookie": {"response-secret"}, "X-Apple-Request-Store-Front": {"143462-1,34"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			},
		})
		Expect(err).To(MatchError(ContainSubstring("unsupported import dyld_lazy_load")))
		data, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		var record struct {
			Body                            string
			StatusCode                      int
			RequestHeaders, ResponseHeaders http.Header
		}
		Expect(json.Unmarshal(data, &record)).To(Succeed())
		Expect(record.Body).To(Equal(body))
		Expect(record.StatusCode).To(Equal(200))
		Expect(record.RequestHeaders.Get("X-Apple-Store-Front")).To(Equal("143462-1,34"))
		Expect(string(data)).NotTo(ContainSubstring("request-secret"))
		Expect(string(data)).NotTo(ContainSubstring("response-secret"))
		info, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
	})
})
