package appstore

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	gohttp "net/http"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/store/emulator"
	"github.com/majd/ipatool/v2/internal/store/profile"
	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"howett.net/plist"
)

var _ = Describe("Terms purchase continuation through the framework", func() {
	DescribeTable("handles the gzip terms action and requires server-confirmed acceptance", func(confirm bool) {
		ctx := context.Background()
		bundle, err := assets.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		prepared, err := profile.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		_, err = writer.Write([]byte(purchaseTermsFixture))
		Expect(err).NotTo(HaveOccurred())
		Expect(writer.Close()).To(Succeed())
		type exchange struct {
			method, endpoint, body string
			header                 gohttp.Header
		}
		var exchanges []exchange
		purchaseCount := 0
		accepted := false
		transport := func(request *gohttp.Request) (*gohttp.Response, error) {
			var body []byte
			if request.Body != nil {
				var readErr error
				body, readErr = io.ReadAll(request.Body)
				if readErr != nil {
					return nil, fmt.Errorf("read fixture request: %w", readErr)
				}
			}
			exchanges = append(exchanges, exchange{method: request.Method, endpoint: request.URL.String(), body: string(body), header: request.Header.Clone()})
			headers := gohttp.Header{"Content-Type": {"application/json"}}
			response := []byte(`{"status":-1}`)
			switch request.URL.Path {
			case PrivateAppStoreAPIPathPurchase:
				purchaseCount++
				headers.Set("Content-Type", "text/xml; charset=UTF-8")
				if purchaseCount == 1 {
					headers.Set("Content-Encoding", "gzip")
					response = compressed.Bytes()
				} else if accepted {
					response = []byte(`<plist version="1.0"><dict><key>jingleDocType</key><string>purchaseSuccess</string><key>status</key><integer>0</integer></dict></plist>`)
				}
			case "/account/terms/info":
				response = []byte(`{"status":0,"termsVersionId":290461,"latestTermsVersionId":290462}`)
			case "/account/terms":
				var data map[string]json.RawMessage
				if json.Unmarshal(body, &data) == nil && string(data["id"]) == `"123456"` && string(data["accepted"]) == "290462" {
					accepted = confirm
					response = []byte(fmt.Sprintf(`{"status":0,"isCurrent":%t}`, confirm))
				}
			}

			return &gohttp.Response{StatusCode: gohttp.StatusOK, Header: headers, Body: io.NopCloser(bytes.NewReader(response))}, nil
		}
		ctrl := gomock.NewController(GinkgoT())
		hardware := machine.NewMockMachine(ctrl)
		hardware.EXPECT().MacAddress().Return("00:01:02:03:04:05", nil)
		bagClient := http.NewMockClient[bagResult](ctrl)
		bagClient.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: 200, Data: bagResult{URLBag: urlBag{TermsInfoEndpoint: "https://amp-account.itunes.apple.com/account/terms/info", TermsCheckEndpoint: "https://buy.itunes.apple.com/account/terms"}}}, nil)
		store := &appstore{
			machine: hardware, bagClient: bagClient, storefrontCountryCodes: map[string]string{"143462": "JP"},
			purchaseClient: http.NewClient[purchaseResult](http.Args{Executor: &termsEmulatorExecutor{image: bundle.AppleMediaServices, prepared: prepared, operation: "purchase", transport: transport}}),
			termsClient:    http.NewClient[termsResult](http.Args{Executor: &termsEmulatorExecutor{image: bundle.AppleMediaServices, prepared: prepared, operation: "request", transport: transport}}),
		}
		err = store.Purchase(PurchaseInput{Account: Account{DirectoryServicesID: "123456", PasswordToken: "fixture-token", StoreFront: "143462-1,34", Pod: "45"}, App: App{ID: 1234}})
		if confirm {
			Expect(err).NotTo(HaveOccurred())
			Expect(exchanges).To(HaveLen(4))
			Expect(purchaseCount).To(Equal(2))
		} else {
			Expect(err).To(MatchError(ContainSubstring("did not confirm acceptance")))
			Expect(exchanges).To(HaveLen(3))
			Expect(purchaseCount).To(Equal(1))
		}
		Expect(exchanges[1].method).To(Equal(gohttp.MethodGet))
		Expect(exchanges[1].endpoint).To(ContainSubstring("cc=JP"))
		Expect(exchanges[2].method).To(Equal(gohttp.MethodPost))
		Expect(exchanges[2].endpoint).To(Equal("https://p45-buy.itunes.apple.com/account/terms"))
		for _, exchange := range exchanges {
			Expect(exchange.header.Get("X-Dsid")).To(Equal("123456"))
			Expect(exchange.header.Get("X-Token")).To(Equal("fixture-token"))
			Expect(exchange.header.Get("X-Apple-Store-Front")).To(Equal("143462-1,34"))
		}
		if confirm {
			var first, last map[string]any
			_, err = plist.Unmarshal([]byte(exchanges[0].body), &first)
			Expect(err).NotTo(HaveOccurred())
			_, err = plist.Unmarshal([]byte(exchanges[3].body), &last)
			Expect(err).NotTo(HaveOccurred())
			for _, key := range []string{"salableAdamId", "price", "productType", "pricingParameters", "guid"} {
				Expect(last[key]).To(Equal(first[key]))
			}
		}
	}, Entry("confirmed acceptance", true), Entry("unconfirmed acceptance", false))
})

type termsEmulatorExecutor struct {
	image     []byte
	prepared  profile.Data
	operation string
	transport func(*gohttp.Request) (*gohttp.Response, error)
}

func (e *termsEmulatorExecutor) Send(request http.Request, _ []byte) (*gohttp.Response, error) {
	guest, err := emulator.Open(context.Background(), e.image, e.prepared)
	if err != nil {
		return nil, fmt.Errorf("open fixture runtime: %w", err)
	}

	defer guest.Close()

	input := emulator.Request{Operation: e.operation, URL: request.URL, Method: request.Method, Format: string(request.ResponseFormat), Headers: gohttp.Header{}, HardwareID: []byte{0, 1, 2, 3, 4, 5}, Bag: map[string]any{"buyProduct": request.URL}, Transport: e.transport}
	for key, value := range request.Headers {
		input.Headers.Set(key, value)
	}

	if payload, ok := request.Payload.(*http.XMLPayload); ok {
		input.Parameters = payload.Content
	}

	if payload, ok := request.Payload.(*http.RawPayload); ok {
		input.Body = payload.Content
	}

	response, err := guest.Execute(input)
	if err != nil {
		return nil, fmt.Errorf("execute fixture request: %w", err)
	}

	return response, nil
}
func (*termsEmulatorExecutor) Do(*gohttp.Request) (*gohttp.Response, error) {
	return nil, errors.New("unexpected direct request")
}
