package cmd

import (
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/majd/ipatool/v2/pkg/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("List Versions command", func() {
	DescribeTable("passes the selected platform to lookup and version listing", func(args []string, expected appstore.Platform, lookup bool) {
		store := &fakeListVersionsAppStore{}
		previous := dependencies
		DeferCleanup(func() { dependencies = previous })
		dependencies.AppStore = store
		dependencies.Logger = log.NewLogger(log.Args{})
		cmd := ListVersionsCmd()
		cmd.SetArgs(args)
		Expect(cmd.Execute()).To(Succeed())
		Expect(store.input.Platform).To(Equal(expected))
		Expect(store.input.App.ID).To(Equal(int64(42)))
		Expect(store.lookupCalled).To(Equal(lookup))
		if lookup {
			Expect(store.lookupInput.Platform).To(Equal(expected))
		}
	},
		Entry("Mac bundle ID", []string{"-b", "com.example.mac", "--platform", "macos"}, appstore.PlatformMacOS, true),
		Entry("Mac app ID alias", []string{"-i", "42", "--platform", "mac"}, appstore.PlatformMacOS, false),
		Entry("default platform", []string{"-i", "42"}, appstore.Platform(""), false),
		Entry("iOS alias", []string{"-i", "42", "--platform", "ios"}, appstore.PlatformIPhone, false),
	)

	It("rejects unknown as a platform before accessing the account", func() {
		cmd := ListVersionsCmd()
		cmd.SetArgs([]string{"-i", "42", "--platform", "unknown"})
		Expect(cmd.Execute()).To(MatchError(`invalid platform "unknown"`))
	})
})

type fakeListVersionsAppStore struct {
	appstore.AppStore
	input        appstore.ListVersionsInput
	lookupInput  appstore.LookupInput
	lookupCalled bool
}

func (*fakeListVersionsAppStore) AccountInfo() (appstore.AccountInfoOutput, error) {
	return appstore.AccountInfoOutput{}, nil
}
func (s *fakeListVersionsAppStore) Lookup(input appstore.LookupInput) (appstore.LookupOutput, error) {
	s.lookupCalled = true
	s.lookupInput = input

	return appstore.LookupOutput{App: appstore.App{ID: 42, BundleID: input.BundleID}}, nil
}
func (s *fakeListVersionsAppStore) ListVersions(input appstore.ListVersionsInput) (appstore.ListVersionsOutput, error) {
	s.input = input

	return appstore.ListVersionsOutput{ExternalVersionIdentifiers: []string{"123"}}, nil
}
