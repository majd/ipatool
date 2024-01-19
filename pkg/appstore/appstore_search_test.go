package appstore

import (
	"errors"

	"github.com/majd/ipatool/v2/pkg/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("AppStore (Search)", func() {
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

	When("request is successful", func() {
		const (
			testID       = 0
			testBundleID = "test-bundle-id"
			testName     = "test-name"
			testVersion  = "test-version"
			testPrice    = 0.0
		)

		BeforeEach(func() {
			mockClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[searchResult]{
					StatusCode: 200,
					Data: searchResult{
						Count: 1,
						Results: []App{
							{
								ID:       testID,
								BundleID: testBundleID,
								Name:     testName,
								Version:  testVersion,
								Price:    testPrice,
							},
						},
					},
				}, nil)
		})

		It("returns output", func() {
			out, err := as.Search(SearchInput{
				Account: Account{
					StoreFront: "143441",
				},
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Count).To(Equal(1))
			Expect(out.Results).To(HaveLen(1))
			Expect(out.Results[0]).To(Equal(App{
				ID:       testID,
				BundleID: testBundleID,
				Name:     testName,
				Version:  testVersion,
				Price:    testPrice,
			}))
		})
	})

	When("store front is invalid", func() {
		It("returns error", func() {
			_, err := as.Search(SearchInput{
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
			_, err := as.Search(SearchInput{
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
			_, err := as.Search(SearchInput{
				Account: Account{
					StoreFront: "143441",
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})
})
