package appstore

import (
	"errors"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("AppStore (ListVersions)", func() {
	var (
		ctrl               *gomock.Controller
		mockDownloadClient *http.MockClient[downloadResult]
		mockMachine        *machine.MockMachine
		as                 AppStore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockDownloadClient = http.NewMockClient[downloadResult](ctrl)
		mockMachine = machine.NewMockMachine(ctrl)
		as = &appstore{
			downloadClient: mockDownloadClient,
			machine:        mockMachine,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("fails to get MAC address", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", errors.New(""))
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("request fails", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{}, errors.New(""))
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("password token is expired", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: FailureTypePasswordTokenExpired,
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("license is required", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: FailureTypeLicenseNotFound,
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("store API returns error with customer message", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType:     "test-failure",
						CustomerMessage: "test error message",
					},
				}, nil)
		})

		It("returns error with customer message", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("test error message"))
		})
	})

	When("store API returns error without customer message", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: "test-failure",
					},
				}, nil)
		})

		It("returns error with failure type", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("test-failure"))
		})
	})

	When("store API returns no items", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{},
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("version identifiers not found in metadata", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								Metadata: map[string]interface{}{
									"someOtherKey": "someValue",
								},
							},
						},
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get version identifiers from item metadata"))
		})
	})

	When("latest version not found in metadata", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								Metadata: map[string]interface{}{
									"softwareVersionExternalIdentifiers": []interface{}{"12345678", "87654321"},
								},
							},
						},
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get latest version from item metadata"))
		})
	})

	When("successfully lists versions", func() {
		const (
			testVersion1 = "12345678"
			testVersion2 = "87654321"
			testLatest   = "87654321"
		)

		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								Metadata: map[string]interface{}{
									"softwareVersionExternalIdentifiers": []interface{}{testVersion1, testVersion2},
									"softwareVersionExternalIdentifier":  testLatest,
								},
							},
						},
					},
				}, nil)
		})

		It("returns versions", func() {
			out, err := as.ListVersions(ListVersionsInput{})
			Expect(err).ToNot(HaveOccurred())
			Expect(out.ExternalVersionIdentifiers).To(Equal([]string{testVersion1, testVersion2}))
			Expect(out.LatestExternalVersionID).To(Equal(testLatest))
		})
	})
})
