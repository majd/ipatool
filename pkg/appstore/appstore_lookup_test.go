package appstore

import (
	"errors"
	"net/url"

	"github.com/majd/ipatool/v2/pkg/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("AppStore (Lookup)", func() {
	var (
		ctrl       *gomock.Controller
		mockClient *http.MockClient[searchResult]
		as         AppStore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = http.NewMockClient[searchResult](ctrl)
		as = &appstore{
			searchClient: mockClient,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	DescribeTable("looks up an app by ID or bundle identifier", func(appID int64, bundleID, key, value string) {
		mockClient.EXPECT().Send(gomock.Any()).DoAndReturn(func(req http.Request) (http.Result[searchResult], error) {
			parsed, err := url.Parse(req.URL)
			Expect(err).NotTo(HaveOccurred())
			Expect(parsed.Query().Get(key)).To(Equal(value))
			Expect(parsed.Query().Get("entity")).To(Equal("macSoftware"))
			Expect(parsed.Query().Get("country")).To(Equal("US"))
			if key == "bundleId" {
				Expect(parsed.Query().Has("id")).To(BeFalse())
			} else {
				Expect(parsed.Query().Has("bundleId")).To(BeFalse())
			}

			return http.Result[searchResult]{
				StatusCode: 200, Data: searchResult{Results: []App{{ID: 123, BundleID: "com.example.app"}}},
			}, nil
		})
		output, err := as.Lookup(LookupInput{
			Account: Account{StoreFront: "143441"}, AppID: appID, BundleID: bundleID, Platform: PlatformMacOS,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(output.App.BundleID).To(Equal("com.example.app"))
	},
		Entry("numeric ID", int64(123), "", "id", "123"),
		Entry("bundle identifier overrides ID", int64(999), "com.example.app", "bundleId", "com.example.app"),
	)

	When("request is successful", func() {
		When("does not find app", func() {
			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[searchResult]{
						StatusCode: 200,
						Data: searchResult{
							Count:   0,
							Results: []App{},
						},
					}, nil)
			})

			It("returns error", func() {
				_, err := as.Lookup(LookupInput{
					Account: Account{
						StoreFront: "143441",
					},
				})
				Expect(errors.Is(err, ErrAppNotFound)).To(BeTrue())
			})
		})

		When("finds app", func() {
			var testApp = App{
				ID:       1,
				BundleID: "app.bundle.id",
				Name:     "app name",
				Version:  "1.0",
				Price:    0.99,
			}

			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[searchResult]{
						StatusCode: 200,
						Data: searchResult{
							Count:   1,
							Results: []App{testApp},
						},
					}, nil)
			})

			It("returns app", func() {
				app, err := as.Lookup(LookupInput{
					Account: Account{
						StoreFront: "143441",
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(app).To(Equal(LookupOutput{App: testApp}))
			})
		})
	})

	When("platform is macOS", func() {
		BeforeEach(func() {
			mockClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					parsedURL, err := url.Parse(req.URL)
					Expect(err).ToNot(HaveOccurred())
					Expect(parsedURL.Query().Get("entity")).To(Equal("macSoftware"))
				}).
				Return(http.Result[searchResult]{}, errors.New("request error"))
		})

		It("uses the macOS lookup entity", func() {
			_, err := as.Lookup(LookupInput{
				Account: Account{
					StoreFront: "143441",
				},
				Platform: PlatformMacOS,
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("platform is AppleTV", func() {
		BeforeEach(func() {
			mockClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					parsedURL, err := url.Parse(req.URL)
					Expect(err).ToNot(HaveOccurred())
					Expect(parsedURL.Query().Get("entity")).To(Equal("tvSoftware"))
				}).
				Return(http.Result[searchResult]{}, errors.New("request error"))
		})

		It("uses the tvOS lookup entity", func() {
			_, err := as.Lookup(LookupInput{
				Account: Account{
					StoreFront: "143441",
				},
				Platform: PlatformAppleTV,
			})
			Expect(err).To(HaveOccurred())
		})
	})

	DescribeTable("checks watchOS support in lookup results", func(platforms []Platform, found bool) {
		mockClient.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
			parsedURL, err := url.Parse(req.URL)
			Expect(err).ToNot(HaveOccurred())
			Expect(parsedURL.Query().Get("entity")).To(Equal("watchSoftware"))
		}).Return(http.Result[searchResult]{StatusCode: 200, Data: searchResult{
			Count: 1, Results: []App{{ID: 42, Platforms: platforms}},
		}}, nil)
		out, err := as.Lookup(LookupInput{Account: Account{StoreFront: "143441"}, AppID: 42, Platform: PlatformWatchOS})
		if found {
			Expect(err).ToNot(HaveOccurred())
			Expect(out.App.ID).To(Equal(int64(42)))
		} else {
			Expect(err).To(MatchError(ErrAppNotFound))
		}
	},
		Entry("Watch app", []Platform{PlatformWatchOS}, true),
		Entry("iPhone app with Watch support", []Platform{PlatformIPhone, PlatformWatchOS}, true),
		Entry("iPhone only", []Platform{PlatformIPhone}, false),
		Entry("unknown platforms", []Platform{PlatformUnknown}, false),
	)

	When("store front is invalid", func() {
		It("returns error", func() {
			_, err := as.Lookup(LookupInput{
				Account: Account{
					StoreFront: "xyz",
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("request fails", func() {
		BeforeEach(func() {
			mockClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[searchResult]{}, errors.New(""))
		})

		It("returns error", func() {
			_, err := as.Lookup(LookupInput{
				Account: Account{
					StoreFront: "143441",
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("request returns bad status code", func() {
		BeforeEach(func() {
			mockClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[searchResult]{
					StatusCode: 400,
				}, nil)
		})

		It("returns error", func() {
			_, err := as.Lookup(LookupInput{
				Account: Account{
					StoreFront: "143441",
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})
})
