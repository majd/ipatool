package cmd

import (
	"errors"

	"github.com/majd/ipatool/v2/pkg/appstore"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MCP app metadata", func() {
	DescribeTable("preserves source data when catalog metadata cannot be used", func(catalog appstore.App, lookupErr error) {
		original := appstore.App{ID: 123, Name: "Original name", Price: 2.99, Platforms: []appstore.Platform{appstore.PlatformUnknown}}
		store := &fakeMCPAppStore{lookup: func(appstore.LookupInput) (appstore.LookupOutput, error) {
			return appstore.LookupOutput{App: catalog}, lookupErr
		}}
		tools := &mcpTools{store: store}

		Expect(tools.completeApp(appstore.Account{}, original, "")).To(Equal(original))
	},
		Entry("delisted app", appstore.App{}, appstore.ErrAppNotFound),
		Entry("network failure", appstore.App{}, errors.New("network failure")),
		Entry("wrong app ID", appstore.App{ID: 999, BundleID: "wrong.bundle"}, nil),
		Entry("missing catalog values", appstore.App{ID: 123}, nil),
	)

	It("avoids catalog lookups when app metadata is already complete", func() {
		tools := &mcpTools{store: &fakeMCPAppStore{}}
		app := appstore.App{ID: 123, BundleID: "com.example.app", Name: "Example", Version: "1.0", Platforms: []appstore.Platform{appstore.PlatformIPhone}}
		Expect(tools.completeApp(appstore.Account{}, app, "")).To(Equal(app))
	})
})
