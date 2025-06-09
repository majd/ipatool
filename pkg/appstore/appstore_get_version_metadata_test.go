package appstore

import (
	"errors"
	"time"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("AppStore (GetVersionMetadata)", func() {
	var (
		ctrl               *gomock.Controller
		mockMachine        *machine.MockMachine
		mockDownloadClient *http.MockClient[downloadResult]
		as                 AppStore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockMachine = machine.NewMockMachine(ctrl)
		mockDownloadClient = http.NewMockClient[downloadResult](ctrl)
		as = &appstore{
			machine:        mockMachine,
			downloadClient: mockDownloadClient,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("fails to get MAC address", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", errors.New("mac error"))
		})

		It("returns error", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get mac address"))
		})
	})

	When("request fails", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{}, errors.New("request error"))
		})

		It("returns error", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to send http request"))
		})
	})

	When("password token is expired", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: FailureTypePasswordTokenExpired,
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
			Expect(err).To(Equal(ErrPasswordTokenExpired))
		})
	})

	When("license is missing", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: FailureTypeLicenseNotFound,
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
			Expect(err).To(Equal(ErrLicenseRequired))
		})
	})

	When("store API returns error", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)
		})

		When("response contains customer message", func() {
			BeforeEach(func() {
				mockDownloadClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[downloadResult]{
						Data: downloadResult{
							FailureType:     "SOME_ERROR",
							CustomerMessage: "Customer error message",
						},
					}, nil)
			})

			It("returns customer message as error", func() {
				_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Customer error message"))
			})
		})

		When("response does not contain customer message", func() {
			BeforeEach(func() {
				mockDownloadClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[downloadResult]{
						Data: downloadResult{
							FailureType: "SOME_ERROR",
						},
					}, nil)
			})

			It("returns generic error", func() {
				_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("SOME_ERROR"))
			})
		})
	})

	When("store API returns no items", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{},
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid response"))
		})
	})

	When("fails to parse release date", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								Metadata: map[string]interface{}{
									"releaseDate": "invalid-date",
								},
							},
						},
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse release date"))
		})
	})

	When("successfully gets version metadata", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								Metadata: map[string]interface{}{
									"releaseDate":              "2024-03-20T12:00:00Z",
									"bundleShortVersionString": "1.0.0",
								},
							},
						},
					},
				}, nil)
		})

		It("returns version metadata", func() {
			output, err := as.GetVersionMetadata(GetVersionMetadataInput{
				Account: Account{
					DirectoryServicesID: "test-dsid",
				},
				App: App{
					ID: 1234567890,
				},
				VersionID: "test-version",
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(output.DisplayVersion).To(Equal("1.0.0"))
			Expect(output.ReleaseDate).To(Equal(time.Date(2024, 3, 20, 12, 0, 0, 0, time.UTC)))
		})
	})
})
