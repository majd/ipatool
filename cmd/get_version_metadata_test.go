package cmd

import (
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/majd/ipatool/v2/pkg/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Get Version Metadata command", func() {
	DescribeTable("passes the platform and exact version to metadata lookup", func(args []string, platform appstore.Platform, lookup bool) {
		store := &fakeVersionMetadataAppStore{}
		previous := dependencies
		DeferCleanup(func() { dependencies = previous })
		dependencies.AppStore = store
		dependencies.Logger = log.NewLogger(log.Args{})
		cmd := getVersionMetadataCmd()
		cmd.SetArgs(append(args, "--external-version-id", "876660716"))
		Expect(cmd.Execute()).To(Succeed())
		Expect(store.input.Platform).To(Equal(platform))
		Expect(store.input.VersionID).To(Equal("876660716"))
		Expect(store.input.App.ID).To(Equal(int64(42)))
		Expect(store.lookupCalled).To(Equal(lookup))
		if lookup {
			Expect(store.lookupInput.Platform).To(Equal(platform))
		}
	},
		Entry("Mac bundle", []string{"-b", "com.example.mac", "--platform", "macos"}, appstore.PlatformMacOS, true),
		Entry("Mac ID alias", []string{"-i", "42", "--platform", "mac"}, appstore.PlatformMacOS, false),
		Entry("default", []string{"-i", "42"}, appstore.Platform(""), false),
	)
	It("rejects unknown as a filter", func() {
		cmd := getVersionMetadataCmd()
		cmd.SetArgs([]string{"-i", "42", "--external-version-id", "123", "--platform", "unknown"})
		Expect(cmd.Execute()).To(MatchError(`invalid platform "unknown"`))
	})
})

type fakeVersionMetadataAppStore struct {
	fakeListVersionsAppStore
	input appstore.GetVersionMetadataInput
}

func (s *fakeVersionMetadataAppStore) GetVersionMetadata(input appstore.GetVersionMetadataInput) (appstore.GetVersionMetadataOutput, error) {
	s.input = input

	return appstore.GetVersionMetadataOutput{}, nil
}
