package appstore

import (
	"errors"

	"github.com/majd/ipatool/v2/pkg/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("AppStore (download endpoint fallback)", func() {
	const redownloadURL = "https://downloaddispatch.itunes.apple.com/r/redownload"

	var (
		ctrl               *gomock.Controller
		mockDownloadClient *http.MockClient[downloadResult]
		st                 *appstore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockDownloadClient = http.NewMockClient[downloadResult](ctrl)
		st = &appstore{downloadClient: mockDownloadClient}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	failure := func(ft string) http.Result[downloadResult] {
		return http.Result[downloadResult]{Data: downloadResult{FailureType: ft}}
	}
	success := func() http.Result[downloadResult] {
		return http.Result[downloadResult]{Data: downloadResult{Items: []downloadItemResult{{URL: "https://example.com/app.ipa"}}}}
	}
	noLongerAvailable := func() http.Result[downloadResult] {
		return http.Result[downloadResult]{Data: downloadResult{CustomerMessage: "“App” No Longer Available"}}
	}

	Describe("sendDownloadProduct", func() {
		When("the volumeStore endpoint returns failureType 5002 and a redownload endpoint is available", func() {
			It("retries on the redownload endpoint with the appExtVrsId version key", func() {
				gomock.InOrder(
					mockDownloadClient.EXPECT().
						Send(gomock.Any()).
						Do(func(req http.Request) {
							Expect(req.URL).To(ContainSubstring(PrivateAppStoreAPIPathDownload))
							payload := req.Payload.(*http.XMLPayload)
							Expect(payload.Content).To(HaveKeyWithValue(downloadVersionKeyVolumeStore, "123"))
							Expect(payload.Content).ToNot(HaveKey(downloadVersionKeyRedownload))
						}).
						Return(failure(FailureTypeLicenseAlreadyExists), nil),
					mockDownloadClient.EXPECT().
						Send(gomock.Any()).
						Do(func(req http.Request) {
							Expect(req.URL).To(HavePrefix(redownloadURL))
							payload := req.Payload.(*http.XMLPayload)
							Expect(payload.Content).To(HaveKeyWithValue(downloadVersionKeyRedownload, "123"))
							Expect(payload.Content).ToNot(HaveKey(downloadVersionKeyVolumeStore))
						}).
						Return(success(), nil),
				)

				res, err := st.sendDownloadProduct(Account{}, App{ID: 42}, "GUID", "123", redownloadURL)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.Data.Items).To(HaveLen(1))
			})
		})

		When("the volumeStore endpoint returns 5002 but no redownload endpoint is configured", func() {
			It("does not retry and returns the original response", func() {
				mockDownloadClient.EXPECT().
					Send(gomock.Any()).
					Return(failure(FailureTypeLicenseAlreadyExists), nil).
					Times(1)

				res, err := st.sendDownloadProduct(Account{}, App{ID: 42}, "GUID", "", "")
				Expect(err).ToNot(HaveOccurred())
				Expect(res.Data.FailureType).To(Equal(FailureTypeLicenseAlreadyExists))
			})
		})

		When("volumeStore returns a transient 5002 that redownload cannot serve", func() {
			It("retries volumeStore and uses its recovered response", func() {
				gomock.InOrder(
					mockDownloadClient.EXPECT().
						Send(gomock.Any()).
						Do(func(req http.Request) { Expect(req.URL).To(ContainSubstring(PrivateAppStoreAPIPathDownload)) }).
						Return(failure(FailureTypeLicenseAlreadyExists), nil),
					mockDownloadClient.EXPECT().
						Send(gomock.Any()).
						Do(func(req http.Request) { Expect(req.URL).To(HavePrefix(redownloadURL)) }).
						Return(noLongerAvailable(), nil),
					mockDownloadClient.EXPECT().
						Send(gomock.Any()).
						Do(func(req http.Request) { Expect(req.URL).To(ContainSubstring(PrivateAppStoreAPIPathDownload)) }).
						Return(success(), nil),
				)

				res, err := st.sendDownloadProduct(Account{}, App{ID: 42}, "GUID", "", redownloadURL)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.Data.Items).To(HaveLen(1))
			})
		})

		When("volumeStore keeps returning 5002 and redownload cannot serve the app", func() {
			It("returns the redownload response so the caller sees the informative message", func() {
				gomock.InOrder(
					mockDownloadClient.EXPECT().Send(gomock.Any()).Return(failure(FailureTypeLicenseAlreadyExists), nil),
					mockDownloadClient.EXPECT().Send(gomock.Any()).Return(noLongerAvailable(), nil),
					mockDownloadClient.EXPECT().Send(gomock.Any()).Return(failure(FailureTypeLicenseAlreadyExists), nil).Times(volumeStoreRetriesAfterFallback),
				)

				res, err := st.sendDownloadProduct(Account{}, App{ID: 42}, "GUID", "", redownloadURL)
				Expect(err).ToNot(HaveOccurred())
				_, itemErr := downloadProductItem(res)
				Expect(itemErr.Error()).To(ContainSubstring("No Longer Available"))
			})
		})

		When("the volumeStore endpoint succeeds", func() {
			It("does not call the redownload endpoint", func() {
				mockDownloadClient.EXPECT().
					Send(gomock.Any()).
					Return(success(), nil).
					Times(1)

				res, err := st.sendDownloadProduct(Account{}, App{ID: 42}, "GUID", "", redownloadURL)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.Data.Items).To(HaveLen(1))
			})
		})

		When("the volumeStore endpoint returns a non-5002 failure", func() {
			It("does not fall back", func() {
				mockDownloadClient.EXPECT().
					Send(gomock.Any()).
					Return(failure(FailureTypeLicenseNotFound), nil).
					Times(1)

				res, err := st.sendDownloadProduct(Account{}, App{ID: 42}, "GUID", "", redownloadURL)
				Expect(err).ToNot(HaveOccurred())
				Expect(res.Data.FailureType).To(Equal(FailureTypeLicenseNotFound))
			})
		})
	})

	Describe("downloadProductItem", func() {
		It("reports a clear error for a surviving 5002 (no redownload endpoint available)", func() {
			_, err := downloadProductItem(http.Result[downloadResult]{Data: downloadResult{
				FailureType:     FailureTypeLicenseAlreadyExists,
				CustomerMessage: "An unknown error has occurred",
			}})
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, ErrPasswordTokenExpired)).To(BeFalse())
			Expect(err.Error()).To(ContainSubstring("redownload endpoint"))
			Expect(err.Error()).ToNot(ContainSubstring("An unknown error has occurred"))
		})

		It("surfaces the customerMessage when the redownload endpoint reports the app unavailable", func() {
			res := http.Result[downloadResult]{Data: downloadResult{CustomerMessage: "“YouTube” No Longer Available"}}
			_, err := downloadProductItem(res)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("No Longer Available"))
		})

		It("maps password-token-expired failures to ErrPasswordTokenExpired", func() {
			_, err := downloadProductItem(failure(FailureTypePasswordTokenExpired))
			Expect(errors.Is(err, ErrPasswordTokenExpired)).To(BeTrue())
		})

		It("maps sign-in-required failures to ErrPasswordTokenExpired", func() {
			_, err := downloadProductItem(failure(FailureTypeSignInRequired))
			Expect(errors.Is(err, ErrPasswordTokenExpired)).To(BeTrue())
		})

		It("maps license-not-found failures to ErrLicenseRequired", func() {
			_, err := downloadProductItem(failure(FailureTypeLicenseNotFound))
			Expect(errors.Is(err, ErrLicenseRequired)).To(BeTrue())
		})

		It("returns the first song-list item on success", func() {
			item, err := downloadProductItem(success())
			Expect(err).ToNot(HaveOccurred())
			Expect(item.URL).To(Equal("https://example.com/app.ipa"))
		})
	})
})
