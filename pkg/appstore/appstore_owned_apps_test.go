package appstore

import (
	"encoding/binary"
	"errors"
	"math"
	gohttp "net/http"
	"time"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("AppStore (OwnedApps)", func() {
	const (
		testDSID       = "123456789"
		testGUID       = "AABBCCDDEEFF"
		testStoreFront = "143441-1,34"
		testToken      = "password-token"
		testSessionID  = uint32(42)
		testRevision   = uint32(99)
	)

	var (
		ctrl            *gomock.Controller
		mockBagClient   *http.MockClient[bagResult]
		mockOwnedClient *http.MockClient[[]byte]
		mockMachine     *machine.MockMachine
		signer          *stubActionSigner
		as              *appstore
	)

	account := Account{
		DirectoryServicesID: testDSID,
		PasswordToken:       testToken,
		StoreFront:          testStoreFront,
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockBagClient = http.NewMockClient[bagResult](ctrl)
		mockOwnedClient = http.NewMockClient[[]byte](ctrl)
		mockMachine = machine.NewMockMachine(ctrl)
		signer = &stubActionSigner{}
		as = &appstore{
			bagClient:       mockBagClient,
			ownedAppsClient: mockOwnedClient,
			machine:         mockMachine,
			actionSignerFactory: func(config SAPConfig, machineID []byte) (ActionSigner, error) {
				Expect(config).To(Equal(validSAPConfig()))
				Expect(machineID).To(Equal([]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}))

				return signer, nil
			},
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	expectSignerSetup := func() {
		mockMachine.EXPECT().
			MacAddress().
			Return("aa:bb:cc:dd:ee:ff", nil)
		mockBagClient.EXPECT().
			Send(gomock.Any()).
			Do(func(req http.Request) {
				Expect(req.URL).To(Equal("https://init.itunes.apple.com/bag.xml?guid=" + testGUID))
			}).
			Return(http.Result[bagResult]{
				StatusCode: gohttp.StatusOK,
				Data:       validBagResult(),
			}, nil)
	}

	expectStorefront := func(storefront string, data []byte) {
		responses := []http.Result[[]byte]{
			{StatusCode: gohttp.StatusOK, Data: dmapTag("mlog", dmapUint32("mlid", testSessionID))},
			{StatusCode: gohttp.StatusOK, Data: dmapTag("mupd", dmapUint32("musr", testRevision))},
			{StatusCode: gohttp.StatusOK, Data: data},
		}
		calls := make([]*gomock.Call, 0, len(responses))
		for _, response := range responses {
			calls = append(calls, mockOwnedClient.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.Headers).To(HaveKeyWithValue("X-Apple-Store-Front", storefront))
			}).Return(response, nil))
		}
		gomock.InOrder(calls[0], calls[1], calls[2])
	}

	DescribeTable("filters merged purchases before sorting and pagination",
		func(platform Platform, page, total int, ids []int64) {
			expectSignerSetup()
			item := func(id, kind, products, date uint32) []byte {
				data := append(dmapUint32("aeSI", id), dmapUint32("aeMk", kind)...)
				data = append(data, dmapUint32("aeSS", products)...)
				data = append(data, dmapUint32("asdp", date)...)

				return dmapTag("mlit", data)
			}
			mobile := append(item(1, 131072, 1, 300), item(2, 131072, 19, 200)...)
			mobile = append(mobile, item(3, 131072, 0, 100)...)
			mac := append(item(2, 67108864, 0, 200), item(4, 67108864, 0, 400)...)
			expectStorefront("143441-1,34", dmapTag("adbs", dmapTag("mlcl", mobile)))
			expectStorefront("143441-1,13", dmapTag("adbs", dmapTag("mlcl", mac)))

			output, err := as.OwnedApps(OwnedAppsInput{Account: account, Platform: platform, Page: page, Limit: 1})

			Expect(err).ToNot(HaveOccurred())
			Expect(output.TotalCount).To(Equal(total))
			Expect(output.Count).To(Equal(len(ids)))
			Expect(output.Page).To(Equal(page))
			actualIDs := make([]int64, 0, len(output.Results))
			for _, app := range output.Results {
				actualIDs = append(actualIDs, app.ID)
			}
			Expect(actualIDs).To(Equal(ids))
			if len(ids) == 1 && ids[0] == 2 {
				Expect(output.Results[0].Platforms).To(Equal([]Platform{PlatformIPhone, PlatformIPad, PlatformVisionOS, PlatformMacOS}))
			}
		},
		Entry("no filter", Platform(""), 1, 4, []int64{4}),
		Entry("iPhone", PlatformIPhone, 1, 2, []int64{1}),
		Entry("iPad", PlatformIPad, 1, 1, []int64{2}),
		Entry("visionOS", PlatformVisionOS, 1, 1, []int64{2}),
		Entry("Mac second page includes merged platform", PlatformMacOS, 2, 2, []int64{2}),
		Entry("no matching Apple TV apps", PlatformAppleTV, 1, 0, []int64{}),
		Entry("page past filtered results", PlatformVisionOS, 2, 1, []int64{}),
	)

	It("puts a new Mac purchase first and merges apps from both storefronts", func() {
		expectSignerSetup()
		mobile := append(dmapUint32("aeSI", 123), dmapUint32("aeSS", 3)...)
		mobile = append(mobile, dmapUint32("aeMk", 131072)...)
		mobile = append(mobile, dmapUint32("asdp", 100)...)
		arcade := append(dmapUint32("aeSI", 456), dmapUint32("aeMk", 262144)...)
		arcade = append(arcade, dmapUint32("aeSS", 3)...)
		mobileListing := append(dmapTag("mlit", mobile), dmapTag("mlit", arcade)...)
		expectStorefront("143441-1,34", dmapTag("adbs", dmapTag("mlcl", mobileListing)))

		// SnippetsLab's Mac media kind and empty supported-products field
		// reproduce the fields that were previously missing from the results.
		mac := append(dmapUint32("aeSI", 1006087419), dmapUint32("aeMk", 67108864)...)
		mac = append(mac, dmapTag("aeSS", []byte{0, 0})...)
		mac = append(mac, dmapString("aeLN", "SnippetsLab")...)
		mac = append(mac, dmapUint32("asdp", 300)...)
		macListing := append(dmapTag("mlit", mobile), dmapTag("mlit", mac)...)
		expectStorefront("143441-1,13", dmapTag("adbs", dmapTag("mlcl", macListing)))

		output, err := as.OwnedApps(OwnedAppsInput{Account: account, Limit: 2})
		Expect(err).ToNot(HaveOccurred())
		Expect(output.TotalCount).To(Equal(3))
		Expect(output.Count).To(Equal(2))
		Expect(output.Results[0].Name).To(Equal("SnippetsLab"))
		Expect(output.Results[0].Platforms).To(Equal([]Platform{PlatformMacOS}))
		Expect(output.Results[1].ID).To(Equal(int64(123)))
		Expect(output.Results[1].Platforms).To(Equal([]Platform{PlatformIPhone, PlatformIPad}))
		Expect(signer.closeCalls).To(Equal(1))
	})

	It("does not infer a platform from the storefront or app name", func() {
		expectSignerSetup()
		expectStorefront("143441-1,34", dmapTag("adbs", nil))
		item := append(dmapUint32("aeSI", 123), dmapString("aeLN", "Example for Mac")...)
		expectStorefront("143441-1,13", dmapTag("adbs", dmapTag("mlcl", dmapTag("mlit", item))))

		output, err := as.OwnedApps(OwnedAppsInput{Account: account})

		Expect(err).ToNot(HaveOccurred())
		Expect(output.Results).To(HaveLen(1))
		Expect(output.Results[0].Platforms).To(Equal([]Platform{PlatformUnknown}))
	})

	It("does not return an incomplete list if the Mac storefront fails", func() {
		expectSignerSetup()
		expectStorefront("143441-1,34", ownedAppsDMAPResponse([]App{{ID: 123}}))
		mockOwnedClient.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: gohttp.StatusUnauthorized}, nil)

		output, err := as.OwnedApps(OwnedAppsInput{Account: account})
		Expect(errors.Is(err, ErrPasswordTokenExpired)).To(BeTrue())
		Expect(output.Results).To(BeEmpty())
		Expect(signer.closeCalls).To(Equal(1))
	})

	When("the account has more than one page of apps", func() {
		It("sorts all apps by descending purchase date before returning the requested partial page", func() {
			expectSignerSetup()

			responseOrder := []int{5, 12, 1, 9, 3, 11, 2, 8, 4, 10, 6, 7}
			apps := make([]App, 0, 12)
			for _, index := range responseOrder {
				apps = append(apps, App{
					ID:           int64(1000 + index),
					BundleID:     "com.example.app" + string(rune('a'+index-1)),
					Name:         "DAAP app",
					Version:      "1.0",
					PurchaseDate: time.Unix(1_700_000_000+int64(index*60), 0).UTC(),
				})
			}

			loginCall := mockOwnedClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					Expect(req.Method).To(Equal(http.MethodPOST))
					Expect(req.URL).To(Equal(PrivatePurchaseDAAPBaseURL + "/login"))
					Expect(req.ActionSigner).To(BeNil())
					Expect(req.Payload).To(BeNil())
					expectOwnedAppsHeaders(req.Headers, account, testGUID)
				}).
				Return(http.Result[[]byte]{
					StatusCode: gohttp.StatusOK,
					Data:       dmapTag("mlog", dmapUint32("mlid", testSessionID)),
				}, nil)

			updateCall := mockOwnedClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					Expect(req.Method).To(Equal(http.MethodPOST))
					Expect(req.URL).To(Equal(PrivatePurchaseDAAPBaseURL + "/update"))
					Expect(req.ActionSigner).To(BeIdenticalTo(signer))
					Expect(req.Headers).To(HaveKeyWithValue("Content-Type", "application/x-www-form-urlencoded"))
					expectOwnedAppsHeaders(req.Headers, account, testGUID)

					payload, ok := req.Payload.(*http.RawPayload)
					Expect(ok).To(BeTrue())
					Expect(string(payload.Content)).To(Equal("session-id=42&revision-number=(null)&query=('com.apple.itunes.extended\\-media\\-kind:131072','com.apple.itunes.extended\\-media\\-kind:262144','com.apple.itunes.extended\\-media\\-kind:67108864')"))
				}).
				Return(http.Result[[]byte]{
					StatusCode: gohttp.StatusOK,
					Data:       dmapTag("mupd", dmapUint32("musr", testRevision)),
				}, nil)

			itemsCall := mockOwnedClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					Expect(req.Method).To(Equal(http.MethodPOST))
					Expect(req.URL).To(Equal(PrivatePurchaseDAAPBaseURL + "/databases/99/items"))
					Expect(req.ActionSigner).To(BeIdenticalTo(signer))
					Expect(req.Headers).To(HaveKeyWithValue("Content-Type", "application/x-dmap-tagged"))
					expectOwnedAppsHeaders(req.Headers, account, testGUID)

					payload, ok := req.Payload.(*http.RawPayload)
					Expect(ok).To(BeTrue())
					sessionID, found, err := firstDMAPUint(payload.Content, "mlid")
					Expect(err).ToNot(HaveOccurred())
					Expect(found).To(BeTrue())
					Expect(sessionID).To(Equal(uint64(testSessionID)))
					revision, found, err := firstDMAPUint(payload.Content, "musr")
					Expect(err).ToNot(HaveOccurred())
					Expect(found).To(BeTrue())
					Expect(revision).To(Equal(uint64(testRevision)))
					err = walkDMAP(payload.Content, 0, func(tag string, data []byte) error {
						if tag == "mque" {
							Expect(string(data)).To(Equal("('com.apple.itunes.extended\\-media\\-kind:131072','com.apple.itunes.extended\\-media\\-kind:262144','com.apple.itunes.extended\\-media\\-kind:67108864')"))
						}

						return nil
					})
					Expect(err).ToNot(HaveOccurred())
				}).
				Return(http.Result[[]byte]{
					StatusCode: gohttp.StatusOK,
					Data:       ownedAppsDMAPResponse(apps),
				}, nil)

			gomock.InOrder(loginCall, updateCall, itemsCall)
			expectStorefront("143441-1,13", ownedAppsDMAPResponse(nil))

			output, err := as.OwnedApps(OwnedAppsInput{
				Account: account,
				Page:    2,
				Limit:   10,
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(output.Count).To(Equal(2))
			Expect(output.TotalCount).To(Equal(12))
			Expect(output.Page).To(Equal(2))
			Expect(output.Results).To(HaveLen(2))
			Expect(output.Results[0].ID).To(Equal(int64(1002)))
			Expect(output.Results[0].PurchaseDate).To(Equal(time.Unix(1_700_000_120, 0).UTC()))
			Expect(output.Results[1].ID).To(Equal(int64(1001)))
			Expect(output.Results[1].PurchaseDate).To(Equal(time.Unix(1_700_000_060, 0).UTC()))
			Expect(signer.closeCalls).To(Equal(1))
		})
	})

	When("the requested page is past the end", func() {
		It("returns an empty page without a metadata lookup", func() {
			expectSignerSetup()

			gomock.InOrder(
				mockOwnedClient.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{
					StatusCode: gohttp.StatusOK,
					Data:       dmapTag("mlog", dmapUint32("mlid", testSessionID)),
				}, nil),
				mockOwnedClient.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{
					StatusCode: gohttp.StatusOK,
					Data:       dmapTag("mupd", dmapUint32("musr", testRevision)),
				}, nil),
				mockOwnedClient.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{
					StatusCode: gohttp.StatusOK,
					Data:       ownedAppsDMAPResponse([]App{{ID: 1}}),
				}, nil),
			)

			expectStorefront("143441-1,13", ownedAppsDMAPResponse(nil))

			output, err := as.OwnedApps(OwnedAppsInput{Account: account, Page: 2, Limit: 10})

			Expect(err).ToNot(HaveOccurred())
			Expect(output.Count).To(Equal(0))
			Expect(output.TotalCount).To(Equal(1))
			Expect(output.Results).To(BeEmpty())
			Expect(signer.closeCalls).To(Equal(1))
		})
	})

	When("the authenticated session is rejected", func() {
		It("returns the password-token-expired error and closes the signer", func() {
			expectSignerSetup()
			mockOwnedClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[[]byte]{StatusCode: gohttp.StatusUnauthorized}, nil)

			_, err := as.OwnedApps(OwnedAppsInput{Account: account})

			Expect(errors.Is(err, ErrPasswordTokenExpired)).To(BeTrue())
			Expect(signer.closeCalls).To(Equal(1))
		})
	})

	When("the purchase history request and signer cleanup both fail", func() {
		It("preserves both errors", func() {
			expectSignerSetup()
			requestErr := errors.New("request error")
			cleanupErr := errors.New("cleanup error")
			signer.closeErr = cleanupErr
			mockOwnedClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[[]byte]{}, requestErr)

			_, err := as.OwnedApps(OwnedAppsInput{Account: account})

			Expect(errors.Is(err, requestErr)).To(BeTrue())
			Expect(errors.Is(err, cleanupErr)).To(BeTrue())
			Expect(signer.closeCalls).To(Equal(1))
		})
	})

	When("the machine address cannot be read", func() {
		It("returns an error before creating a signer", func() {
			mockMachine.EXPECT().MacAddress().Return("", errors.New("machine error"))

			_, err := as.OwnedApps(OwnedAppsInput{Account: account})

			Expect(err).To(MatchError(ContainSubstring("failed to get mac address")))
		})
	})

	DescribeTable("validates and defaults pagination",
		func(input OwnedAppsInput, expected OwnedAppsInput, errorText string) {
			actual, err := normalizeOwnedAppsInput(input)
			if errorText != "" {
				Expect(err).To(MatchError(ContainSubstring(errorText)))

				return
			}

			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
		},
		Entry("defaults page and limit", OwnedAppsInput{}, OwnedAppsInput{Page: 1, Limit: 10}, ""),
		Entry("accepts the maximum limit", OwnedAppsInput{Page: 3, Limit: 100}, OwnedAppsInput{Page: 3, Limit: 100}, ""),
		Entry("rejects a negative page", OwnedAppsInput{Page: -1, Limit: 10}, OwnedAppsInput{}, "page"),
		Entry("rejects a negative limit", OwnedAppsInput{Page: 1, Limit: -1}, OwnedAppsInput{}, "limit"),
		Entry("rejects a limit over 100", OwnedAppsInput{Page: 1, Limit: 101}, OwnedAppsInput{}, "100"),
		Entry("rejects an invalid platform", OwnedAppsInput{Platform: "invalid"}, OwnedAppsInput{}, "invalid platform"),
		Entry("rejects unknown as a filter", OwnedAppsInput{Platform: PlatformUnknown}, OwnedAppsInput{}, "invalid platform"),
	)
})

var _ = Describe("owned-app DMAP parsing", func() {
	It("parses app fields, ignores unknown fields, and removes duplicate IDs", func() {
		item := make([]byte, 0, 128)
		item = append(item, dmapString("zzzz", "unknown")...)
		item = append(item, dmapAppIDForTest(123)...)
		item = append(item, dmapString("aeBI", "com.example.app")...)
		item = append(item, dmapString("aeLN", "Example")...)
		item = append(item, dmapString("aePd", "1.2.3")...)
		item = append(item, dmapUint32("asdp", 1_700_000_000)...)
		listing := append(dmapTag("mlit", item), dmapTag("mlit", item)...)
		response := dmapTag("adbs", dmapTag("mlcl", listing))

		apps, err := parseOwnedApps(response)

		Expect(err).ToNot(HaveOccurred())
		Expect(apps).To(Equal([]App{{
			ID:           123,
			BundleID:     "com.example.app",
			Name:         "Example",
			Version:      "1.2.3",
			PurchaseDate: time.Unix(1_700_000_000, 0).UTC(),
			Platforms:    []Platform{PlatformUnknown},
		}}))
	})

	DescribeTable("decodes supported platforms",
		func(kind uint32, products []byte, expected []Platform) {
			item := append(dmapUint32("aeMk", kind), dmapTag("aeSS", products)...)
			app, err := parseOwnedApp(item)
			Expect(err).ToNot(HaveOccurred())
			Expect(app.Platforms).To(Equal(expected))
		},
		Entry("iPhone", uint32(131072), []byte{0, 1}, []Platform{PlatformIPhone}),
		Entry("iPad", uint32(131072), []byte{0, 2}, []Platform{PlatformIPad}),
		Entry("Mac universal purchase", uint32(131072), []byte{0, 8}, []Platform{PlatformMacOS}),
		Entry("visionOS", uint32(131072), []byte{0, 16}, []Platform{PlatformVisionOS}),
		Entry("Arcade does not imply Mac support", uint32(262144), []byte{0, 3}, []Platform{PlatformIPhone, PlatformIPad}),
		Entry("universal app", uint32(131072), []byte{0, 27}, []Platform{PlatformIPhone, PlatformIPad, PlatformVisionOS, PlatformMacOS}),
		Entry("Mac ignores mobile product bits", uint32(67108864), []byte{0, 7}, []Platform{PlatformMacOS}),
		Entry("iPod touch only", uint32(131072), []byte{0, 4}, []Platform{PlatformUnknown}),
		Entry("unknown product bit does not imply visionOS", uint32(131072), []byte{0, 32}, []Platform{PlatformUnknown}),
		Entry("empty products", uint32(131072), []byte{0, 0}, []Platform{PlatformUnknown}),
		Entry("unknown media kind", uint32(999), []byte{0, 31}, []Platform{PlatformUnknown}),
		Entry("unknown products", uint32(131072), []byte{128, 0}, []Platform{PlatformUnknown}),
		Entry("one-byte products", uint32(131072), []byte{3}, []Platform{PlatformIPhone, PlatformIPad}),
		Entry("four-byte products", uint32(131072), []byte{0, 0, 0, 3}, []Platform{PlatformIPhone, PlatformIPad}),
		Entry("eight-byte products", uint32(131072), []byte{0, 0, 0, 0, 0, 0, 0, 3}, []Platform{PlatformIPhone, PlatformIPad}),
	)

	DescribeTable("returns unknown for missing platform metadata",
		func(item []byte) {
			app, err := parseOwnedApp(item)
			Expect(err).ToNot(HaveOccurred())
			Expect(app.Platforms).To(Equal([]Platform{PlatformUnknown}))
		},
		Entry("missing products", dmapUint32("aeMk", 131072)),
		Entry("missing media kind", dmapUint32("aeSS", 31)),
	)

	DescribeTable("replaces unknown when another record identifies the platform",
		func(platforms ...[]Platform) {
			records := make([]App, 0, len(platforms))
			for _, value := range platforms {
				records = append(records, App{ID: 123, Platforms: value})
			}
			apps := mergeOwnedApps(records)
			Expect(apps).To(HaveLen(1))
			Expect(apps[0].Platforms).To(Equal([]Platform{PlatformMacOS}))
		},
		Entry("unknown first", []Platform{PlatformUnknown}, []Platform{PlatformMacOS}),
		Entry("unknown last", []Platform{PlatformMacOS}, []Platform{PlatformUnknown}),
	)

	It("identifies YouTube for visionOS as visionOS only", func() {
		item := dmapAppIDForTest(6745572359)
		item = append(item, dmapString("aeBI", "com.google.visionosyoutube")...)
		item = append(item, dmapString("aeLN", "YouTube for visionOS")...)
		item = append(item, dmapUint32("aeMk", 131072)...)
		item = append(item, dmapTag("aeSS", []byte{0, 16})...)

		apps, err := parseOwnedApps(dmapTag("adbs", dmapTag("mlcl", dmapTag("mlit", item))))

		Expect(err).ToNot(HaveOccurred())
		Expect(apps).To(HaveLen(1))
		Expect(apps[0].ID).To(Equal(int64(6745572359)))
		Expect(apps[0].Platforms).To(Equal([]Platform{PlatformVisionOS}))
	})

	It("merges platforms and keeps the latest purchase date before pagination", func() {
		mobile := append(dmapUint32("aeSI", 123), dmapUint32("aeSS", 3)...)
		mobile = append(mobile, dmapUint32("aeMk", 131072)...)
		mobile = append(mobile, dmapUint32("asdp", 100)...)
		mac := append(dmapUint32("aeSI", 123), dmapUint32("aeMk", 67108864)...)
		mac = append(mac, dmapUint32("asdp", 300)...)
		other := append(dmapUint32("aeSI", 456), dmapUint32("asdp", 200)...)
		listing := append(dmapTag("mlit", mobile), dmapTag("mlit", other)...)
		listing = append(listing, dmapTag("mlit", mac)...)
		listing = append(listing, dmapTag("mlit", mobile)...)

		apps, err := parseOwnedApps(dmapTag("adbs", dmapTag("mlcl", listing)))
		Expect(err).ToNot(HaveOccurred())
		Expect(apps).To(HaveLen(2))
		page := ownedAppsPage(ownedAppsSortedByPurchaseDate(apps), 1, 1)
		Expect(page[0].ID).To(Equal(int64(123)))
		Expect(page[0].Platforms).To(Equal([]Platform{PlatformIPhone, PlatformIPad, PlatformMacOS}))
		Expect(page[0].PurchaseDate).To(Equal(time.Unix(300, 0).UTC()))
	})

	DescribeTable("rejects malformed platform metadata",
		func(tag string) {
			_, err := parseOwnedApp(dmapTag(tag, []byte{1, 2, 3}))
			Expect(err).To(MatchError(ContainSubstring("invalid integer length 3")))
		},
		Entry("media kind", "aeMk"),
		Entry("supported products", "aeSS"),
	)

	It("rejects a malformed purchase date", func() {
		item := append(dmapAppIDForTest(123), dmapTag("asdp", []byte{1, 2, 3})...)
		response := dmapTag("adbs", dmapTag("mlcl", dmapTag("mlit", item)))

		_, err := parseOwnedApps(response)

		Expect(err).To(MatchError(ContainSubstring("purchase date has invalid length 3")))
	})

	It("rejects a truncated tag", func() {
		_, err := parseOwnedApps([]byte("adbs\x00\x00\x00\x10short"))

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exceeds remaining response length"))
	})

	It("rejects a protocol-level error status", func() {
		response := dmapTag("mlog", dmapUint32("mstt", gohttp.StatusForbidden))

		err := ownedAppsDMAPStatusError("purchase history login", response)

		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, ErrPasswordTokenExpired)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("DAAP status 403"))
	})

	It("preserves a partial last page", func() {
		apps := []App{{ID: 1}, {ID: 2}, {ID: 3}}

		Expect(ownedAppsPage(apps, 2, 2)).To(Equal([]App{{ID: 3}}))
		Expect(ownedAppsPage(apps, 3, 2)).To(BeEmpty())
		Expect(ownedAppsPage(apps, math.MaxInt, 100)).To(BeEmpty())
	})

	It("sorts missing dates last and preserves response order for ties", func() {
		date := time.Unix(1_700_000_000, 0).UTC()
		apps := []App{
			{ID: 1},
			{ID: 2, PurchaseDate: date},
			{ID: 3, PurchaseDate: date.Add(time.Hour)},
			{ID: 4, PurchaseDate: date},
			{ID: 5},
		}

		sorted := ownedAppsSortedByPurchaseDate(apps)

		Expect(sorted).To(Equal([]App{
			{ID: 3, PurchaseDate: date.Add(time.Hour)},
			{ID: 2, PurchaseDate: date},
			{ID: 4, PurchaseDate: date},
			{ID: 1},
			{ID: 5},
		}))
		Expect(apps[0].ID).To(Equal(int64(1)))
	})

	It("builds the dynamic DMAP item request body", func() {
		now := time.Unix(1_700_000_000, 0)
		query := "('com.apple.itunes.extended\\-media\\-kind:131072')"
		body := ownedAppsItemsBody(42, 99, query, now)

		timestamp, found, err := firstDMAPUint(body, "mstc")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(timestamp).To(Equal(uint64(1_700_000_000)))
		sessionID, found, err := firstDMAPUint(body, "mlid")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(sessionID).To(Equal(uint64(42)))
	})
})

func expectOwnedAppsHeaders(headers map[string]string, account Account, guid string) {
	Expect(headers).To(HaveKeyWithValue("X-Dsid", account.DirectoryServicesID))
	Expect(headers).To(HaveKeyWithValue("iCloud-DSID", account.DirectoryServicesID))
	Expect(headers).To(HaveKeyWithValue("X-Token", account.PasswordToken))
	Expect(headers).To(HaveKeyWithValue("X-Apple-Store-Front", account.StoreFront))
	Expect(headers).To(HaveKeyWithValue("X-Guid", guid))
	Expect(headers).To(HaveKeyWithValue("Client-DAAP-Version", "3.12"))
	Expect(headers).To(HaveKeyWithValue("Client-Cloud-Purchase-Daap-Version", "1.1/Configurator-2.0"))
}

func ownedAppsDMAPResponse(apps []App) []byte {
	listing := make([]byte, 0, len(apps)*128)

	for _, app := range apps {
		item := make([]byte, 0, 128)
		item = append(item, dmapAppIDForTest(uint64(app.ID))...)
		item = append(item, dmapString("aeBI", app.BundleID)...)
		item = append(item, dmapString("aeLN", app.Name)...)
		item = append(item, dmapString("aePd", app.Version)...)

		if !app.PurchaseDate.IsZero() {
			item = append(item, dmapUint32("asdp", uint32(app.PurchaseDate.Unix()))...)
		}

		listing = append(listing, dmapTag("mlit", item)...)
	}

	return dmapTag("adbs", dmapTag("mlcl", listing))
}

func dmapAppIDForTest(value uint64) []byte {
	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, value)

	return dmapTag("aeSI", payload)
}
