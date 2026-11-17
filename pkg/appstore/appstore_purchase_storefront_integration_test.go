package appstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	gohttp "net/http"
	"strings"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/store/emulator"
	"github.com/majd/ipatool/v2/internal/store/profile"
	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("CountryMismatch framework response", func() {
	It("recovers the reported dialog through the real encoder and response interpreter", func() {
		ctx := context.Background()
		bundle, err := assets.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		prepared, err := profile.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		account := Account{DirectoryServicesID: "123456", StoreFront: "143441-1,34", PasswordToken: "fixture-token", Pod: "30"}
		data, err := json.Marshal(account)
		Expect(err).NotTo(HaveOccurred())
		chain := &storefrontTestKeychain{data: data}
		var storefronts, endpoints []string
		executor := &storefrontEmulatorExecutor{
			image: bundle.AppleMediaServices, prepared: prepared,
			transport: func(request *gohttp.Request) (*gohttp.Response, error) {
				storefronts = append(storefronts, request.Header.Get("X-Apple-Store-Front"))
				endpoints = append(endpoints, request.URL.String())
				body := countryMismatchFixture
				if len(storefronts) > 1 {
					body = `<plist version="1.0"><dict><key>jingleDocType</key><string>purchaseSuccess</string><key>status</key><integer>0</integer></dict></plist>`
				}

				return &gohttp.Response{StatusCode: gohttp.StatusOK, Header: gohttp.Header{"Content-Type": {"text/xml; charset=UTF-8"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			},
		}
		hardware := machine.NewMockMachine(gomock.NewController(GinkgoT()))
		hardware.EXPECT().MacAddress().Return("00:01:02:03:04:05", nil)
		store := &appstore{keychain: chain, machine: hardware, purchaseClient: http.NewClient[purchaseResult](http.Args{Executor: executor})}
		Expect(store.Purchase(PurchaseInput{Account: account, App: App{ID: 1234}})).To(Succeed())
		Expect(storefronts).To(Equal([]string{"143441-1,34", "143462-1,34"}))
		Expect(endpoints).To(HaveEach("https://p30-buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/buyProduct"))
		info, err := store.AccountInfo()
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Account.StoreFront).To(Equal("143462-1,34"))
	})
})

type storefrontEmulatorExecutor struct {
	image     []byte
	prepared  profile.Data
	transport func(*gohttp.Request) (*gohttp.Response, error)
}

func (e *storefrontEmulatorExecutor) Send(request http.Request, _ []byte) (*gohttp.Response, error) {
	guest, err := emulator.Open(context.Background(), e.image, e.prepared)
	if err != nil {
		return nil, fmt.Errorf("open fixture runtime: %w", err)
	}

	defer guest.Close()

	headers := gohttp.Header{}
	for key, value := range request.Headers {
		headers.Set(key, value)
	}

	response, err := guest.Execute(emulator.Request{
		Operation: "purchase", URL: request.URL, Method: request.Method, Headers: headers,
		HardwareID: []byte{0, 1, 2, 3, 4, 5}, Parameters: request.Payload.(*http.XMLPayload).Content,
		Bag: map[string]any{"buyProduct": request.URL}, Transport: e.transport,
	})
	if err != nil {
		return nil, fmt.Errorf("execute fixture request: %w", err)
	}

	return response, nil
}
func (*storefrontEmulatorExecutor) Do(*gohttp.Request) (*gohttp.Response, error) {
	return nil, errors.New("unexpected direct request")
}
