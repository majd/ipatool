package cmd

import (
	"context"
	"errors"

	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/majd/ipatool/v2/pkg/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Download command", func() {
	It("exposes macOS in platform help", func() {
		cmd := downloadCmd()
		Expect(cmd.Flag("platform").Usage).To(ContainSubstring("macos"))
	})

	It("skips sinf replication for macOS packages", func() {
		replicator := &fakeSinfReplicator{}
		err := replicateDownloadSinf(replicator, appstore.PlatformMacOS, appstore.DownloadOutput{
			DestinationPath: "app.pkg",
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(replicator.called).To(BeFalse())
	})

	It("replicates sinf for non-macOS packages", func() {
		replicator := &fakeSinfReplicator{}
		out := appstore.DownloadOutput{
			DestinationPath: "app.ipa",
			Sinfs:           []appstore.Sinf{{Data: []byte("sinf")}},
		}

		err := replicateDownloadSinf(replicator, appstore.PlatformIPhone, out)

		Expect(err).ToNot(HaveOccurred())
		Expect(replicator.called).To(BeTrue())
		Expect(replicator.input).To(Equal(appstore.ReplicateSinfInput{
			PackagePath: out.DestinationPath,
			Sinfs:       out.Sinfs,
		}))
	})

	It("returns sinf replication errors for non-macOS packages", func() {
		replicator := &fakeSinfReplicator{err: errors.New("replication failed")}
		err := replicateDownloadSinf(replicator, appstore.PlatformIPhone, appstore.DownloadOutput{})
		Expect(err).To(MatchError("replication failed"))
	})

	It("propagates macOS when automatic purchase retries the download", func() {
		store := &fakeDownloadAppStore{downloadErrors: []error{appstore.ErrLicenseRequired, nil}}
		previousDependencies := dependencies
		DeferCleanup(func() { dependencies = previousDependencies })
		dependencies.Logger = log.NewLogger(log.Args{})

		cmd := downloadCmdWithAppStore(func() appstore.AppStore { return store })
		cmd.SetArgs([]string{"--app-id", "42", "--platform", "macos", "--purchase"})
		cmd.SetContext(context.WithValue(context.Background(), interactiveKey, false))

		Expect(cmd.Execute()).To(Succeed())
		Expect(store.purchaseInputs).To(HaveLen(1))
		Expect(store.purchaseInputs[0].Platform).To(Equal(appstore.PlatformMacOS))
		Expect(store.purchaseInputs[0].App).To(Equal(appstore.App{ID: 42}))
		Expect(store.downloadInputs).To(HaveLen(2))
		Expect(store.downloadInputs[0].Platform).To(Equal(appstore.PlatformMacOS))
		Expect(store.downloadInputs[1].Platform).To(Equal(appstore.PlatformMacOS))
	})

	It("retains the pending automatic purchase after refreshing authentication", func() {
		store := &fakeDownloadAppStore{
			downloadErrors: []error{appstore.ErrLicenseRequired, nil},
			purchaseErrors: []error{appstore.ErrPasswordTokenExpired, nil},
		}
		previousDependencies := dependencies
		DeferCleanup(func() { dependencies = previousDependencies })
		dependencies.Logger = log.NewLogger(log.Args{})

		cmd := downloadCmdWithAppStore(func() appstore.AppStore { return store })
		cmd.SetArgs([]string{"--app-id", "42", "--platform", "macos", "--purchase"})
		cmd.SetContext(context.WithValue(context.Background(), interactiveKey, false))

		Expect(cmd.Execute()).To(Succeed())
		Expect(store.loginInputs).To(HaveLen(1))
		Expect(store.purchaseInputs).To(HaveLen(2))
		Expect(store.purchaseInputs[0].Platform).To(Equal(appstore.PlatformMacOS))
		Expect(store.purchaseInputs[1].Platform).To(Equal(appstore.PlatformMacOS))
		Expect(store.downloadInputs).To(HaveLen(2))
	})
})

type fakeSinfReplicator struct {
	called bool
	input  appstore.ReplicateSinfInput
	err    error
}

func (f *fakeSinfReplicator) ReplicateSinf(input appstore.ReplicateSinfInput) error {
	f.called = true
	f.input = input

	return f.err
}

type fakeDownloadAppStore struct {
	downloadErrors []error
	downloadInputs []appstore.DownloadInput
	loginInputs    []appstore.LoginInput
	purchaseErrors []error
	purchaseInputs []appstore.PurchaseInput
}

func (f *fakeDownloadAppStore) Login(input appstore.LoginInput) (appstore.LoginOutput, error) {
	f.loginInputs = append(f.loginInputs, input)

	return appstore.LoginOutput{Account: appstore.Account{StoreFront: "143441"}}, nil
}

func (*fakeDownloadAppStore) AccountInfo() (appstore.AccountInfoOutput, error) {
	return appstore.AccountInfoOutput{Account: appstore.Account{StoreFront: "143441"}}, nil
}

func (*fakeDownloadAppStore) Revoke() error { return nil }

func (*fakeDownloadAppStore) Lookup(input appstore.LookupInput) (appstore.LookupOutput, error) {
	return appstore.LookupOutput{}, nil
}

func (*fakeDownloadAppStore) Search(input appstore.SearchInput) (appstore.SearchOutput, error) {
	return appstore.SearchOutput{}, nil
}

func (*fakeDownloadAppStore) OwnedApps(input appstore.OwnedAppsInput) (appstore.OwnedAppsOutput, error) {
	return appstore.OwnedAppsOutput{}, nil
}

func (f *fakeDownloadAppStore) Purchase(input appstore.PurchaseInput) error {
	f.purchaseInputs = append(f.purchaseInputs, input)
	index := len(f.purchaseInputs) - 1

	if index < len(f.purchaseErrors) && f.purchaseErrors[index] != nil {
		return f.purchaseErrors[index]
	}

	return nil
}

func (f *fakeDownloadAppStore) Download(input appstore.DownloadInput) (appstore.DownloadOutput, error) {
	f.downloadInputs = append(f.downloadInputs, input)
	index := len(f.downloadInputs) - 1

	if index < len(f.downloadErrors) && f.downloadErrors[index] != nil {
		return appstore.DownloadOutput{}, f.downloadErrors[index]
	}

	return appstore.DownloadOutput{DestinationPath: "app.pkg"}, nil
}

func (*fakeDownloadAppStore) ReplicateSinf(input appstore.ReplicateSinfInput) error { return nil }

func (*fakeDownloadAppStore) ListVersions(input appstore.ListVersionsInput) (appstore.ListVersionsOutput, error) {
	return appstore.ListVersionsOutput{}, nil
}

func (*fakeDownloadAppStore) GetVersionMetadata(input appstore.GetVersionMetadataInput) (appstore.GetVersionMetadataOutput, error) {
	return appstore.GetVersionMetadataOutput{}, nil
}

func (*fakeDownloadAppStore) Bag(input appstore.BagInput) (appstore.BagOutput, error) {
	return appstore.BagOutput{}, nil
}
