package appstore

import (
	"errors"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("AppStore (VersionHistory)", func() {
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

	When("request is successful", func() {
		const (
			testAppID         = int64(389801252)
			testBundleID      = "com.test.app"
			testAppName       = "Test App"
			testLatestVersion = "1.2.3"
			testVersionID1    = uint64(1001)
			testVersionID2    = uint64(1002)
			testVersionStr1   = "1.1.0"
			testVersionStr2   = "1.2.0"
			testMacAddress    = "AA:BB:CC:DD:EE:FF"
		)

		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return(testMacAddress, nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					StatusCode: 200,
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								Metadata: map[string]interface{}{
									"bundleDisplayName":                  testAppName,
									"bundleIdentifier":                   testBundleID,
									"bundleShortVersionString":           testLatestVersion,
									"softwareVersionExternalIdentifiers": []interface{}{testVersionID1, testVersionID2},
								},
							},
						},
					},
				}, nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					StatusCode: 200,
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								Metadata: map[string]interface{}{
									"bundleShortVersionString": testVersionStr1,
								},
							},
						},
					},
				}, nil).
				Times(1)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					StatusCode: 200,
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								Metadata: map[string]interface{}{
									"bundleShortVersionString": testVersionStr2,
								},
							},
						},
					},
				}, nil).
				Times(1)
		})

		It("returns version history output", func() {
			out, err := as.VersionHistory(VersionHistoryInput{
				Account: Account{
					DirectoryServicesID: "test-dsid",
				},
				App: App{
					ID: testAppID,
				},
				MaxCount: 10,
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(out.VersionHistory.App.ID).To(Equal(testAppID))
			Expect(out.VersionHistory.App.Name).To(Equal(testAppName))
			Expect(out.VersionHistory.App.BundleID).To(Equal(testBundleID))
			Expect(out.VersionHistory.LatestVersion).To(Equal(testLatestVersion))
			Expect(out.VersionHistory.VersionIdentifiers).To(HaveLen(2))
			Expect(out.VersionHistory.VersionIdentifiers[0]).To(Equal("1001"))
			Expect(out.VersionHistory.VersionIdentifiers[1]).To(Equal("1002"))
			Expect(out.VersionDetails).To(HaveLen(2))
			Expect(out.VersionDetails[0].Success).To(BeTrue())
			Expect(out.VersionDetails[1].Success).To(BeTrue())
		})
	})

	When("fails to get MAC address", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", errors.New("mac address error"))
		})

		It("returns error", func() {
			_, err := as.VersionHistory(VersionHistoryInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get mac address"))
		})
	})

	When("request fails", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("AA:BB:CC:DD:EE:FF", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{}, errors.New("request failed"))
		})

		It("returns error", func() {
			_, err := as.VersionHistory(VersionHistoryInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to send http request"))
		})
	})

	When("password token is expired", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("AA:BB:CC:DD:EE:FF", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: FailureTypePasswordTokenExpired,
					},
				}, nil)
		})

		It("returns password token expired error", func() {
			_, err := as.VersionHistory(VersionHistoryInput{})
			Expect(err).To(Equal(ErrPasswordTokenExpired))
		})
	})

	When("API returns error", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("AA:BB:CC:DD:EE:FF", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType:     "test-failure",
						CustomerMessage: "Test error message",
					},
				}, nil)
		})

		It("returns error with customer message", func() {
			_, err := as.VersionHistory(VersionHistoryInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Test error message"))
		})
	})

	When("no app data found", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("AA:BB:CC:DD:EE:FF", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{},
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.VersionHistory(VersionHistoryInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no app data found"))
		})
	})
})
