package appstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/byteness/keyring"
	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"howett.net/plist"
)

// Relevant fields from the decoded response in issue #666.
const countryMismatchFixture = `<plist version="1.0"><dict>
<key>metrics</key><dict><key>dialogId</key><string>MZCommerce.CountryMismatch</string></dict>
<key>failureType</key><string>-128</string>
<key>customerMessage</key><string>Account Not In This Store</string>
<key>dialog</key><dict>
<key>explanation</key><string>Your account is not valid for use in the U.S. store. You must switch to the Japanese store before purchasing.</string>
<key>okButtonAction</key><dict><key>kind</key><string>Goto</string>
<key>url</key><string>https://itunes.apple.com/WebObjects/MZStore.woa/wa/resetAndRedirect?dsf=143462</string></dict>
</dict></dict></plist>`

var _ = Describe("Account storefront recovery", func() {
	var store *appstore
	var chain *storefrontTestKeychain
	var client *http.MockClient[purchaseResult]
	var loginClient *http.MockClient[loginResult]
	var account Account
	var mismatch http.Result[purchaseResult]
	var input PurchaseInput
	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		client = http.NewMockClient[purchaseResult](ctrl)
		loginClient = http.NewMockClient[loginResult](ctrl)
		hardware := machine.NewMockMachine(ctrl)
		hardware.EXPECT().MacAddress().Return("00:01:02:03:04:05", nil).AnyTimes()
		account = Account{Email: "user@example.com", DirectoryServicesID: "123456", StoreFront: "143441-1,34", PasswordToken: "old-token", Password: "password", Pod: "30", Name: "Test User"}
		data, err := json.Marshal(account)
		Expect(err).NotTo(HaveOccurred())
		chain = &storefrontTestKeychain{data: data}
		store = &appstore{keychain: chain, purchaseClient: client, loginClient: loginClient, machine: hardware}
		input = PurchaseInput{Account: account, App: App{ID: 1234}}
		mismatch = http.Result[purchaseResult]{StatusCode: 200}
		_, err = plist.Unmarshal([]byte(countryMismatchFixture), &mismatch.Data)
		Expect(err).NotTo(HaveOccurred())
	})

	DescribeTable("recovers using Apple's account country while preserving client suffixes", func(identifier string) {
		mismatch.Data.Dialog.OKButtonAction.URL = strings.Replace(mismatch.Data.Dialog.OKButtonAction.URL, "143462", identifier, 1)
		calls := 0
		var first http.Request
		client.EXPECT().Send(gomock.Any()).DoAndReturn(func(request http.Request) (http.Result[purchaseResult], error) {
			calls++
			if calls == 1 {
				first = request

				return mismatch, nil
			}
			Expect(request.URL).To(Equal(first.URL))
			Expect(request.Method).To(Equal(first.Method))
			Expect(request.Payload).To(Equal(first.Payload))
			Expect(request.Headers["X-Apple-Store-Front"]).To(Equal(identifier + "-1,34"))
			Expect(request.Headers["X-Token"]).To(Equal("old-token"))
			info, err := store.AccountInfo()
			Expect(err).NotTo(HaveOccurred())
			expected := account
			expected.StoreFront = identifier + "-1,34"
			Expect(info.Account).To(Equal(expected))

			return http.Result[purchaseResult]{StatusCode: 200, Data: purchaseResult{JingleDocType: "purchaseSuccess"}}, nil
		}).Times(2)
		Expect(store.Purchase(input)).To(Succeed())
		Expect(chain.writes).To(Equal(1))
	}, Entry("JP", "143462"), Entry("AE", "143481"), Entry("TR", "143480"))

	It("preserves a correction across token refresh and a new appstore instance", func() {
		calls := 0
		client.EXPECT().Send(gomock.Any()).DoAndReturn(func(request http.Request) (http.Result[purchaseResult], error) {
			calls++
			if calls == 1 {
				return mismatch, nil
			}
			Expect(request.Headers["X-Apple-Store-Front"]).To(Equal("143462-1,34"))
			if calls == 2 {
				return http.Result[purchaseResult]{Data: purchaseResult{FailureType: FailureTypePasswordTokenExpired}}, nil
			}
			Expect(request.Headers["X-Token"]).To(Equal("new-token"))

			return http.Result[purchaseResult]{StatusCode: 200, Data: purchaseResult{JingleDocType: "purchaseSuccess"}}, nil
		}).Times(3)
		Expect(errors.Is(store.Purchase(input), ErrPasswordTokenExpired)).To(BeTrue())
		loginClient.EXPECT().Send(gomock.Any()).Return(http.Result[loginResult]{StatusCode: 200,
			Headers: map[string]string{HTTPHeaderRequestStoreFront: "143441-1,34", HTTPHeaderPod: "45"},
			Data:    loginResult{PasswordToken: "new-token", DirectoryServicesID: account.DirectoryServicesID},
		}, nil)
		refreshed := &appstore{keychain: chain, loginClient: loginClient, purchaseClient: client, machine: store.machine}
		acc, err := refreshed.login(account.Email, account.Password, "", "000102030405", testAuthEndpoint, &stubActionSigner{})
		Expect(err).NotTo(HaveOccurred())
		Expect(acc.StoreFront).To(Equal("143462-1,34"))
		Expect(acc.PasswordToken).To(Equal("new-token"))
		Expect(acc.Pod).To(Equal("45"))
		info, err := refreshed.AccountInfo()
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Account).To(Equal(acc))
		input.Account = acc
		Expect(refreshed.Purchase(input)).To(Succeed())
	})

	It("stops after one correction even if Apple proposes another country", func() {
		second := mismatch
		second.Data.Dialog.OKButtonAction.URL = strings.Replace(second.Data.Dialog.OKButtonAction.URL, "143462", "143480", 1)
		gomock.InOrder(client.EXPECT().Send(gomock.Any()).Return(mismatch, nil), client.EXPECT().Send(gomock.Any()).Return(second, nil))
		Expect(store.Purchase(input)).To(MatchError(ContainSubstring("Account Not In This Store")))
		Expect(chain.writes).To(Equal(1))
		info, err := store.AccountInfo()
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Account.StoreFront).To(Equal("143462-1,34"))
	})

	It("preserves the corrected storefront for the Arcade fallback", func() {
		gomock.InOrder(
			client.EXPECT().Send(gomock.Any()).Return(mismatch, nil),
			client.EXPECT().Send(gomock.Any()).Return(http.Result[purchaseResult]{Data: purchaseResult{FailureType: FailureTypeTemporarilyUnavailable}}, nil),
			client.EXPECT().Send(gomock.Any()).DoAndReturn(func(request http.Request) (http.Result[purchaseResult], error) {
				Expect(request.Headers["X-Apple-Store-Front"]).To(Equal("143462-1,34"))
				Expect(request.Payload.(*http.XMLPayload).Content["pricingParameters"]).To(Equal(PricingParameterAppleArcade))

				return http.Result[purchaseResult]{Data: purchaseResult{JingleDocType: "purchaseSuccess"}}, nil
			}),
		)
		Expect(store.Purchase(input)).To(Succeed())
	})

	DescribeTable("does not replay a purchase when its correction cannot be persisted", func(kind string) {
		switch kind {
		case "read":
			chain.readErr = errors.New("keychain read failed")
		case "write":
			chain.writeErr = errors.New("keychain write failed")
		case "different account":
			chain.data = []byte(`{"directoryServicesIdentifier":"other","storeFront":"143480"}`)
		case "missing identity":
			input.Account.DirectoryServicesID = ""
		}
		original := string(chain.data)
		client.EXPECT().Send(gomock.Any()).Return(mismatch, nil).Times(1)
		Expect(store.Purchase(input)).To(MatchError(ContainSubstring("failed to correct account storefront")))
		Expect(string(chain.data)).To(Equal(original))
		Expect(chain.writes).To(BeZero())
	}, Entry("read error", "read"), Entry("write error", "write"), Entry("account changed", "different account"), Entry("missing DSID", "missing identity"))

	It("retains saved credentials when the caller holds an older account copy", func() {
		saved := account
		saved.PasswordToken = "newer-saved-token"
		var err error
		chain.data, err = json.Marshal(saved)
		Expect(err).NotTo(HaveOccurred())
		failure := errors.New("connection lost")
		gomock.InOrder(client.EXPECT().Send(gomock.Any()).Return(mismatch, nil), client.EXPECT().Send(gomock.Any()).Return(http.Result[purchaseResult]{}, failure))
		Expect(errors.Is(store.Purchase(input), failure)).To(BeTrue())
		info, err := store.AccountInfo()
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Account.PasswordToken).To(Equal(saved.PasswordToken))
		Expect(info.Account.StoreFront).To(Equal("143462-1,34"))
	})

	DescribeTable("ignores malformed or unrelated store-switch URLs", func(actionURL string) {
		mismatch.Data.Dialog.OKButtonAction.URL = actionURL
		client.EXPECT().Send(gomock.Any()).Return(mismatch, nil).Times(1)
		Expect(store.Purchase(input)).To(HaveOccurred())
		Expect(chain.writes).To(BeZero())
	},
		Entry("different host", "https://example.com/WebObjects/MZStore.woa/wa/resetAndRedirect?dsf=143462"),
		Entry("host suffix", "https://itunes.apple.com.example.com/WebObjects/MZStore.woa/wa/resetAndRedirect?dsf=143462"),
		Entry("HTTP", "http://itunes.apple.com/WebObjects/MZStore.woa/wa/resetAndRedirect?dsf=143462"),
		Entry("credentials", "https://user@itunes.apple.com/WebObjects/MZStore.woa/wa/resetAndRedirect?dsf=143462"),
		Entry("port", "https://itunes.apple.com:8080/WebObjects/MZStore.woa/wa/resetAndRedirect?dsf=143462"),
		Entry("terms", "https://itunes.apple.com/WebObjects/MZFinance.woa/wa/termsPage?dsf=143462"),
		Entry("encoded path", "https://itunes.apple.com/WebObjects/MZStore.woa/wa/%72esetAndRedirect?dsf=143462"),
		Entry("missing country", "https://itunes.apple.com/WebObjects/MZStore.woa/wa/resetAndRedirect"),
		Entry("empty country", "https://itunes.apple.com/WebObjects/MZStore.woa/wa/resetAndRedirect?dsf="),
		Entry("duplicate country", "https://itunes.apple.com/WebObjects/MZStore.woa/wa/resetAndRedirect?dsf=143462&dsf=143480"),
		Entry("invalid query", "https://itunes.apple.com/WebObjects/MZStore.woa/wa/resetAndRedirect?dsf=%zz"),
		Entry("zero", "https://itunes.apple.com/WebObjects/MZStore.woa/wa/resetAndRedirect?dsf=0"),
		Entry("overflow", "https://itunes.apple.com/WebObjects/MZStore.woa/wa/resetAndRedirect?dsf=99999999999999999999"),
		Entry("suffix in country", "https://itunes.apple.com/WebObjects/MZStore.woa/wa/resetAndRedirect?dsf=143462-1,34"),
		Entry("fragment", "https://itunes.apple.com/WebObjects/MZStore.woa/wa/resetAndRedirect?dsf=143462#other"),
		Entry("unchanged country", "https://itunes.apple.com/WebObjects/MZStore.woa/wa/resetAndRedirect?dsf=143441"),
	)

	DescribeTable("requires an explicit CountryMismatch rejection", func(kind string) {
		switch kind {
		case "metrics":
			mismatch.Data.Metrics.DialogID = "MZCommerce.AccountDisabled"
		case "failure":
			mismatch.Data.FailureType = "other"
		case "action":
			mismatch.Data.Dialog.OKButtonAction.Kind = "OpenURL"
		case "status":
			mismatch.StatusCode = 403
		}
		client.EXPECT().Send(gomock.Any()).Return(mismatch, nil).Times(1)
		Expect(store.Purchase(input)).To(HaveOccurred())
		Expect(chain.writes).To(BeZero())
	}, Entry("unrelated dialog", "metrics"), Entry("unrelated failure", "failure"), Entry("unrelated action", "action"), Entry("non-success HTTP status", "status"))
})

var _ = Describe("Login storefront selection", func() {
	DescribeTable("keeps saved storefronts only for the same account when Apple supplies an echo", func(savedID, explicit, echo, expected string) {
		chain := &storefrontTestKeychain{}
		if savedID != "" {
			var err error
			chain.data, err = json.Marshal(Account{DirectoryServicesID: savedID, StoreFront: "143462-1,34"})
			Expect(err).NotTo(HaveOccurred())
		}
		ctrl := gomock.NewController(GinkgoT())
		client := http.NewMockClient[loginResult](ctrl)
		headers := map[string]string{}
		if explicit != "" {
			headers[HTTPHeaderStoreFront] = explicit
		}
		if echo != "" {
			headers[HTTPHeaderRequestStoreFront] = echo
		}
		client.EXPECT().Send(gomock.Any()).Return(http.Result[loginResult]{StatusCode: 200, Headers: headers, Data: loginResult{DirectoryServicesID: "123456", PasswordToken: "new-token"}}, nil)
		store := &appstore{keychain: chain, loginClient: client}
		account, err := store.login("user@example.com", "password", "", "000102030405", testAuthEndpoint, &stubActionSigner{})
		Expect(err).NotTo(HaveOccurred())
		Expect(account.StoreFront).To(Equal(expected))
		Expect(account.PasswordToken).To(Equal("new-token"))
	},
		Entry("same account, misleading echo", "123456", "", "143441-1,34", "143462-1,34"),
		Entry("same account, missing storefront headers", "123456", "", "", "143462-1,34"),
		Entry("authoritative update", "123456", "143480-1,34", "143441-1,34", "143480-1,34"),
		Entry("different account", "654321", "", "143441-1,34", "143441-1,34"),
		Entry("first login", "", "", "143441-1,34", "143441-1,34"),
	)

	It("does not overwrite saved state when reading it fails", func() {
		chain := &storefrontTestKeychain{readErr: errors.New("keychain unavailable")}
		store := &appstore{keychain: chain}
		_, err := store.loginStorefront(http.Result[loginResult]{Headers: map[string]string{HTTPHeaderRequestStoreFront: "143441"}}, "123456")
		Expect(err).To(MatchError(ContainSubstring("keychain unavailable")))
		Expect(chain.writes).To(BeZero())
	})
})

type storefrontTestKeychain struct {
	data              []byte
	readErr, writeErr error
	writes            int
}

func (k *storefrontTestKeychain) Get(key string) ([]byte, error) {
	if k.readErr != nil {
		return nil, k.readErr
	}

	if key != "account" || k.data == nil {
		return nil, keyring.ErrKeyNotFound
	}

	return append([]byte(nil), k.data...), nil
}
func (k *storefrontTestKeychain) Set(key string, data []byte) error {
	if k.writeErr != nil {
		return k.writeErr
	}

	if key != "account" {
		return fmt.Errorf("unexpected key %q", key)
	}

	k.data = append([]byte(nil), data...)
	k.writes++

	return nil
}
func (k *storefrontTestKeychain) Remove(string) error {
	k.data = nil

	return nil
}
