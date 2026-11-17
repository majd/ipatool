package appstore

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"howett.net/plist"
)

const purchaseTermsFixture = `<plist version="1.0"><dict><key>pings</key><array></array><key>jingleDocType</key><string>buyProductFailure</string><key>jingleAction</key><string>buyProduct</string><key>action</key><dict><key>kind</key><string>Goto</string><key>url</key><string>https://p45-buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/com.apple.jingle.app.finance.DirectAction/termsPage</string><key>target</key><string>main</string></dict></dict></plist>`

var _ = Describe("Automatic purchase terms acceptance", func() {
	var store *appstore
	var purchases *http.MockClient[purchaseResult]
	var termClient *http.MockClient[termsResult]
	var bags *http.MockClient[bagResult]
	var input PurchaseInput
	var blocked, success http.Result[purchaseResult]
	zero := 0
	info := http.Result[termsResult]{StatusCode: 200, Data: termsResult{Status: &zero, TermsVersionID: json.RawMessage(`290462`)}}
	accepted := http.Result[termsResult]{StatusCode: 200, Data: termsResult{Status: &zero, IsCurrent: true}}
	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		purchases = http.NewMockClient[purchaseResult](ctrl)
		termClient = http.NewMockClient[termsResult](ctrl)
		bags = http.NewMockClient[bagResult](ctrl)
		hardware := machine.NewMockMachine(ctrl)
		hardware.EXPECT().MacAddress().Return("00:01:02:03:04:05", nil).AnyTimes()
		store = &appstore{purchaseClient: purchases, termsClient: termClient, bagClient: bags, machine: hardware, storefrontCountryCodes: testStorefrontCountryCodes()}
		store.storefrontCountryCodes["143462"] = "JP"
		input = PurchaseInput{Account: Account{DirectoryServicesID: "123456", StoreFront: "143462-1,34", PasswordToken: "fixture-token", Pod: "45"}, App: App{ID: 1234}}
		blocked = http.Result[purchaseResult]{StatusCode: 200}
		_, err := plist.Unmarshal([]byte(purchaseTermsFixture), &blocked.Data)
		Expect(err).NotTo(HaveOccurred())
		success = http.Result[purchaseResult]{StatusCode: 200, Data: purchaseResult{JingleDocType: "purchaseSuccess"}}
	})
	expectBag := func() {
		bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: 200, Data: bagResult{
			TermsInfoEndpoint:  "https://amp-account.itunes.apple.com/account/terms/info",
			TermsCheckEndpoint: "https://buy.itunes.apple.com/account/terms",
		}}, nil)
	}

	It("retrieves the account country's current version, accepts it, and retries the unchanged purchase", func() {
		var original http.Request
		calls := 0
		purchases.EXPECT().Send(gomock.Any()).DoAndReturn(func(request http.Request) (http.Result[purchaseResult], error) {
			calls++
			if calls == 1 {
				original = request

				return blocked, nil
			}
			Expect(request).To(Equal(original))

			return success, nil
		}).Times(2)
		expectBag()
		gomock.InOrder(
			termClient.EXPECT().Send(gomock.Any()).DoAndReturn(func(request http.Request) (http.Result[termsResult], error) {
				u, err := url.Parse(request.URL)
				Expect(err).NotTo(HaveOccurred())
				Expect(u.Host).To(Equal("amp-account.itunes.apple.com"))
				Expect(u.Query().Get("cc")).To(Equal("JP"))
				Expect(u.Query().Get("termsType")).To(Equal("MZStore.NewTermsAndConditionsStatement"))
				Expect(request.Method).To(Equal(http.MethodGET))
				Expect(request.Headers["X-Apple-Store-Front"]).To(Equal(input.Account.StoreFront))
				Expect(request.Headers["X-Token"]).To(Equal(input.Account.PasswordToken))

				return info, nil
			}),
			termClient.EXPECT().Send(gomock.Any()).DoAndReturn(func(request http.Request) (http.Result[termsResult], error) {
				Expect(request.Method).To(Equal(http.MethodPOST))
				Expect(request.URL).To(Equal("https://p45-buy.itunes.apple.com/account/terms"))
				Expect(request.Headers["X-Dsid"]).To(Equal(input.Account.DirectoryServicesID))
				Expect(request.Headers["Content-Type"]).To(Equal("application/json"))
				var data map[string]json.RawMessage
				Expect(json.Unmarshal(request.Payload.(*http.RawPayload).Content, &data)).To(Succeed())
				Expect(string(data["id"])).To(Equal(`"123456"`))
				Expect(string(data["accepted"])).To(Equal(`290462`))

				return accepted, nil
			}),
		)
		Expect(store.Purchase(input)).To(Succeed())
	})

	It("preserves storefront recovery before accepting terms", func() {
		input.Account.StoreFront = "143441-1,34"
		saved, err := json.Marshal(input.Account)
		Expect(err).NotTo(HaveOccurred())
		store.keychain = &storefrontTestKeychain{data: saved}
		mismatch := http.Result[purchaseResult]{StatusCode: 200}
		_, err = plist.Unmarshal([]byte(countryMismatchFixture), &mismatch.Data)
		Expect(err).NotTo(HaveOccurred())
		gomock.InOrder(purchases.EXPECT().Send(gomock.Any()).Return(mismatch, nil), purchases.EXPECT().Send(gomock.Any()).Return(blocked, nil), purchases.EXPECT().Send(gomock.Any()).DoAndReturn(func(request http.Request) (http.Result[purchaseResult], error) {
			Expect(request.Headers["X-Apple-Store-Front"]).To(Equal("143462-1,34"))

			return success, nil
		}))
		expectBag()
		gomock.InOrder(termClient.EXPECT().Send(gomock.Any()).DoAndReturn(func(request http.Request) (http.Result[termsResult], error) {
			Expect(request.URL).To(ContainSubstring("cc=JP"))

			return info, nil
		}), termClient.EXPECT().Send(gomock.Any()).Return(accepted, nil))
		Expect(store.Purchase(input)).To(Succeed())
	})

	It("does not loop if Apple still requires terms after confirming acceptance", func() {
		purchases.EXPECT().Send(gomock.Any()).Return(blocked, nil).Times(2)
		expectBag()
		gomock.InOrder(termClient.EXPECT().Send(gomock.Any()).Return(info, nil), termClient.EXPECT().Send(gomock.Any()).Return(accepted, nil))
		Expect(store.Purchase(input)).To(MatchError(ContainSubstring("still requires terms acceptance")))
	})

	It("does not submit acceptance when the terms version is missing", func() {
		purchases.EXPECT().Send(gomock.Any()).Return(blocked, nil).Times(1)
		expectBag()
		termClient.EXPECT().Send(gomock.Any()).Return(http.Result[termsResult]{StatusCode: 200, Data: termsResult{Status: &zero}}, nil).Times(1)
		Expect(store.Purchase(input)).To(MatchError(ContainSubstring("invalid terms version")))
	})

	DescribeTable("does not retry the purchase without confirmed acceptance", func(result http.Result[termsResult], failure error) {
		purchases.EXPECT().Send(gomock.Any()).Return(blocked, nil).Times(1)
		expectBag()
		gomock.InOrder(termClient.EXPECT().Send(gomock.Any()).Return(info, nil), termClient.EXPECT().Send(gomock.Any()).Return(result, failure))
		Expect(store.Purchase(input)).To(HaveOccurred())
	},
		Entry("not current", http.Result[termsResult]{StatusCode: 200, Data: termsResult{Status: &zero}}, nil),
		Entry("missing status", http.Result[termsResult]{StatusCode: 200, Data: termsResult{IsCurrent: true}}, nil),
		Entry("HTTP failure", http.Result[termsResult]{StatusCode: 500, Data: accepted.Data}, nil),
		Entry("transport failure", http.Result[termsResult]{}, errors.New("fixture network failure")),
	)

	DescribeTable("preserves expired-token recovery during the terms flow", func(expiredAtAcceptance bool) {
		purchases.EXPECT().Send(gomock.Any()).Return(blocked, nil).Times(1)
		expectBag()
		expiration := http.Result[termsResult]{StatusCode: 200, Data: termsResult{ErrorMessageKey: "MZFinance.TermsCheckLoginRequired"}}
		if expiredAtAcceptance {
			gomock.InOrder(termClient.EXPECT().Send(gomock.Any()).Return(info, nil), termClient.EXPECT().Send(gomock.Any()).Return(expiration, nil))
		} else {
			termClient.EXPECT().Send(gomock.Any()).Return(expiration, nil)
		}
		Expect(errors.Is(store.Purchase(input), ErrPasswordTokenExpired)).To(BeTrue())
	}, Entry("retrieving terms", false), Entry("accepting terms", true))

	DescribeTable("does not act on unrelated or invalid purchase actions", func(kind, document, endpoint string) {
		blocked.Data.Action.Kind = kind
		blocked.Data.Action.URL = endpoint
		blocked.Data.JingleDocType = document
		purchases.EXPECT().Send(gomock.Any()).Return(blocked, nil).Times(1)
		Expect(store.Purchase(input)).To(HaveOccurred())
	},
		Entry("different action", "OpenURL", "buyProductFailure", "https://p45-buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/com.apple.jingle.app.finance.DirectAction/termsPage"),
		Entry("different document", "Goto", "other", "https://p45-buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/com.apple.jingle.app.finance.DirectAction/termsPage"),
		Entry("different host", "Goto", "buyProductFailure", "https://example.com/WebObjects/MZFinance.woa/wa/com.apple.jingle.app.finance.DirectAction/termsPage"),
		Entry("different page", "Goto", "buyProductFailure", "https://p45-buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/accountSummary"),
	)
})

var _ = Describe("Terms protocol validation", func() {
	DescribeTable("uses the current version without changing its JSON type", func(current, latest, want string) {
		result, err := termsVersion(termsResult{TermsVersionID: json.RawMessage(current), LatestTermsVersionID: json.RawMessage(latest)})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(result)).To(Equal(want))
	}, Entry("numeric version", "290462", "", "290462"), Entry("string version", `"290462"`, "", `"290462"`), Entry("newer version", "290462", "290463", "290463"))
	DescribeTable("rejects invalid versions before acceptance", func(value string) {
		_, err := termsVersion(termsResult{TermsVersionID: json.RawMessage(value)})
		Expect(err).To(HaveOccurred())
	}, Entry("missing", ""), Entry("null", "null"), Entry("zero", "0"), Entry("negative", "-1"), Entry("fraction", "1.5"), Entry("object", "{}"), Entry("boolean", "true"), Entry("text", `"unknown"`))
	DescribeTable("rejects untrusted or unrelated bag endpoints", func(endpoint string, acceptance bool) {
		_, err := termsEndpoint(endpoint, "45", acceptance)
		Expect(err).To(HaveOccurred())
	}, Entry("external info", "https://example.com/account/terms/info", false), Entry("HTTP info", "http://amp-account.itunes.apple.com/account/terms/info", false), Entry("info credentials", "https://user@amp-account.itunes.apple.com/account/terms/info", false), Entry("external acceptance", "https://example.com/account/terms", true), Entry("unrelated acceptance", "https://buy.itunes.apple.com/account/delete", true), Entry("host suffix", "https://buy.itunes.apple.com.example.com/account/terms", true), Entry("non-numbered pod", "https://evil-buy.itunes.apple.com/account/terms", true), Entry("encoded path", "https://buy.itunes.apple.com/account/%74erms", true))
	It("does not expose action URLs in existing purchase error metadata", func() {
		result := purchaseResult{Action: loginDialogActionResult{Kind: "Goto", URL: "https://example.com/?token=secret"}}
		data, err := json.Marshal(result)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.Contains(string(data), "secret")).To(BeFalse())
	})
})

var _ = Describe("Terms endpoints in URL bags", func() {
	DescribeTable("retains terms endpoints without changing authentication endpoint selection", func(data bagResult, auth, info, check string) {
		client := http.NewMockClient[bagResult](gomock.NewController(GinkgoT()))
		client.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: 200, Data: data}, nil)
		store := &appstore{bagClient: client}
		bag, err := store.fetchURLBag("000102030405")
		Expect(err).NotTo(HaveOccurred())
		Expect(bag.AuthEndpoint).To(Equal(auth))
		Expect(bag.TermsInfoEndpoint).To(Equal(info))
		Expect(bag.TermsCheckEndpoint).To(Equal(check))
	},
		Entry("flat terms with nested authentication", bagResult{URLBag: urlBag{AuthEndpoint: "nested-auth"}, TermsInfoEndpoint: "info", TermsCheckEndpoint: "check"}, "nested-auth", "info", "check"),
		Entry("nested terms with flat authentication", bagResult{URLBag: urlBag{TermsInfoEndpoint: "info", TermsCheckEndpoint: "check"}, AuthEndpoint: "flat-auth"}, "flat-auth", "info", "check"),
		Entry("nested terms take precedence", bagResult{URLBag: urlBag{TermsInfoEndpoint: "nested-info", TermsCheckEndpoint: "nested-check"}, TermsInfoEndpoint: "flat-info", TermsCheckEndpoint: "flat-check"}, "", "nested-info", "nested-check"),
	)
})
