package cmd

import (
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/majd/ipatool/v2/pkg/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Purchase command", func() {
	It("accepts a platform flag with macOS help", func() {
		cmd := purchaseCmd()
		platform, err := cmd.Flags().GetString("platform")

		Expect(err).ToNot(HaveOccurred())
		Expect(platform).To(BeEmpty())
		Expect(cmd.Flag("platform").Usage).To(ContainSubstring("macos"))
	})

	It("propagates macOS to lookup and purchase", func() {
		store := &fakePurchaseAppStore{}
		previousDependencies := dependencies
		DeferCleanup(func() { dependencies = previousDependencies })
		dependencies.Logger = log.NewLogger(log.Args{})

		cmd := purchaseCmdWithAppStore(func() appstore.AppStore { return store })
		cmd.SetArgs([]string{"--bundle-identifier", "com.example.mac", "--platform", "macos"})

		Expect(cmd.Execute()).To(Succeed())
		Expect(store.lookupInput.BundleID).To(Equal("com.example.mac"))
		Expect(store.lookupInput.Platform).To(Equal(appstore.PlatformMacOS))
		Expect(store.purchaseInput.Platform).To(Equal(appstore.PlatformMacOS))
		Expect(store.purchaseInput.App).To(Equal(appstore.App{ID: 42, BundleID: "com.example.mac"}))
	})
})

type fakePurchaseAppStore struct {
	lookupInput   appstore.LookupInput
	purchaseInput appstore.PurchaseInput
}

func (*fakePurchaseAppStore) Login(input appstore.LoginInput) (appstore.LoginOutput, error) {
	return appstore.LoginOutput{}, nil
}

func (*fakePurchaseAppStore) AccountInfo() (appstore.AccountInfoOutput, error) {
	return appstore.AccountInfoOutput{Account: appstore.Account{StoreFront: "143441"}}, nil
}

func (*fakePurchaseAppStore) Revoke() error { return nil }

func (f *fakePurchaseAppStore) Lookup(input appstore.LookupInput) (appstore.LookupOutput, error) {
	f.lookupInput = input

	return appstore.LookupOutput{App: appstore.App{ID: 42, BundleID: input.BundleID}}, nil
}

func (*fakePurchaseAppStore) Search(input appstore.SearchInput) (appstore.SearchOutput, error) {
	return appstore.SearchOutput{}, nil
}

func (*fakePurchaseAppStore) OwnedApps(input appstore.OwnedAppsInput) (appstore.OwnedAppsOutput, error) {
	return appstore.OwnedAppsOutput{}, nil
}

func (f *fakePurchaseAppStore) Purchase(input appstore.PurchaseInput) error {
	f.purchaseInput = input

	return nil
}

func (*fakePurchaseAppStore) Download(input appstore.DownloadInput) (appstore.DownloadOutput, error) {
	return appstore.DownloadOutput{}, nil
}

func (*fakePurchaseAppStore) ReplicateSinf(input appstore.ReplicateSinfInput) error { return nil }

func (*fakePurchaseAppStore) ListVersions(input appstore.ListVersionsInput) (appstore.ListVersionsOutput, error) {
	return appstore.ListVersionsOutput{}, nil
}

func (*fakePurchaseAppStore) GetVersionMetadata(input appstore.GetVersionMetadataInput) (appstore.GetVersionMetadataOutput, error) {
	return appstore.GetVersionMetadataOutput{}, nil
}

func (*fakePurchaseAppStore) Bag(input appstore.BagInput) (appstore.BagOutput, error) {
	return appstore.BagOutput{}, nil
}
