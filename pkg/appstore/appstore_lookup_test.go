package appstore

import (
	"github.com/golang/mock/gomock"
	"github.com/majd/ipatool/pkg/http"
	"github.com/majd/ipatool/pkg/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"os"
)

var _ = Describe("AppStore (Lookup)", func() {
	var (
		ctrl       *gomock.Controller
		mockClient *http.MockClient[SearchResult]
		mockLogger *log.MockLogger
		as         *appstore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = http.NewMockClient[SearchResult](ctrl)
		mockLogger = log.NewMockLogger(ctrl)
		as = &appstore{
			searchClient: mockClient,
			ioReader:     os.Stdin,
			logger:       mockLogger,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("country code is invalid", func() {
		It("returns error", func() {
			_, err := as.lookup("", "XYZ", "")
			Expect(err).To(MatchError(ContainSubstring("invalid country code")))
		})
	})

	When("device family is invalid", func() {
		It("returns error", func() {
			_, err := as.lookup("", "US", "XYZ")
			Expect(err).To(MatchError(ContainSubstring("device family is not supported: XYZ")))
		})
	})

	When("request fails", func() {
		var testErr = errors.New("test")

		BeforeEach(func() {
			mockClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{}, testErr)
		})

		It("returns error", func() {
			_, err := as.lookup("", "US", DeviceFamilyPhone)
			Expect(err).To(MatchError(ContainSubstring(testErr.Error())))
			Expect(err).To(MatchError(ContainSubstring("lookup request failed")))
		})
	})

	When("request returns bad status code", func() {
		BeforeEach(func() {
			mockLogger.EXPECT().
				Debug().
				Return(nil)

			mockClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 400,
				}, nil)
		})

		It("returns error", func() {
			_, err := as.lookup("", "US", DeviceFamilyPad)
			Expect(err).To(MatchError(ContainSubstring("lookup request failed with status 400")))
		})
	})

	When("request is successful", func() {
		When("does not find app", func() {
			BeforeEach(func() {
				mockClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[SearchResult]{
						StatusCode: 200,
						Data: SearchResult{
							Count:   0,
							Results: []App{},
						},
					}, nil)
			})

			It("returns error", func() {
				_, err := as.lookup("", "US", DeviceFamilyPad)
				Expect(err).To(MatchError(ContainSubstring("app not found")))
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
					Return(http.Result[SearchResult]{
						StatusCode: 200,
						Data: SearchResult{
							Count:   1,
							Results: []App{testApp},
						},
					}, nil)
			})

			It("returns app", func() {
				app, err := as.lookup("", "US", DeviceFamilyPhone)
				Expect(err).ToNot(HaveOccurred())
				Expect(app).To(Equal(testApp))
			})
		})
	})
})
