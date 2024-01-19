package appstore

import (
	"errors"

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
				Expect(err).To(HaveOccurred())
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
