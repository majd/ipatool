package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/majd/ipatool/v2/pkg/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Download command", func() {
	Describe("app resolution", func() {
		var store *fakeDownloadAppStore

		BeforeEach(func() {
			store = &fakeDownloadAppStore{account: appstore.Account{StoreFront: "143441"}}
			previousDependencies := dependencies
			DeferCleanup(func() { dependencies = previousDependencies })
			dependencies.Logger = log.NewLogger(log.Args{})
		})

		execute := func(args ...string) error {
			cmd := downloadCmdWithAppStore(func() appstore.AppStore { return store })
			cmd.SetArgs(args)
			cmd.SetContext(context.WithValue(context.Background(), interactiveKey, false))

			return cmd.Execute()
		}

		It("uses the app ID when the bundle is absent from the catalog", func() {
			store.lookupError = fmt.Errorf("lookup: %w", appstore.ErrAppNotFound)
			Expect(execute("-i", "42", "-b", "com.example.delisted", "--platform", "appletv")).To(Succeed())
			Expect(store.lookupInputs).To(Equal([]appstore.LookupInput{{
				Account:  appstore.Account{StoreFront: "143441"},
				BundleID: "com.example.delisted",
				Platform: appstore.PlatformAppleTV,
			}}))
			Expect(store.downloadInputs).To(HaveLen(1))
			Expect(store.downloadInputs[0].App).To(Equal(appstore.App{ID: 42, BundleID: "com.example.delisted"}))
			Expect(store.downloadInputs[0].Platform).To(Equal(appstore.PlatformAppleTV))
		})

		It("preserves bundle identifier precedence when lookup succeeds", func() {
			store.lookupOutput.App = appstore.App{ID: 43, BundleID: "com.example.listed"}
			Expect(execute("-i", "42", "-b", "com.example.listed")).To(Succeed())
			Expect(store.downloadInputs).To(HaveLen(1))
			Expect(store.downloadInputs[0].App).To(Equal(store.lookupOutput.App))
		})

		It("resolves a bundle identifier without an app ID", func() {
			store.lookupOutput.App = appstore.App{ID: 43, BundleID: "com.example.listed"}
			Expect(execute("-b", "com.example.listed")).To(Succeed())
			Expect(store.downloadInputs).To(HaveLen(1))
			Expect(store.downloadInputs[0].App).To(Equal(store.lookupOutput.App))
		})

		It("does not look up the bundle for an explicit app ID and version", func() {
			Expect(execute("-i", "42", "--platform", "appletv", "--external-version-id", "123456")).To(Succeed())
			Expect(store.lookupInputs).To(BeEmpty())
			Expect(store.downloadInputs).To(HaveLen(1))
			Expect(store.downloadInputs[0].App).To(Equal(appstore.App{ID: 42}))
			Expect(store.downloadInputs[0].ExternalVersionID).To(Equal("123456"))
		})

		DescribeTable("preserves lookup errors", func(args []string, lookupError error) {
			store.lookupError = lookupError
			Expect(execute(args...)).To(MatchError(lookupError))
			Expect(store.downloadInputs).To(BeEmpty())
		},
			Entry("missing bundle without an app ID", []string{"-b", "com.example.delisted"}, appstore.ErrAppNotFound),
			Entry("request failure with an app ID", []string{"-i", "42", "-b", "com.example.app"}, errors.New("request failed")),
		)
	})

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

	It("replicates sinf for mobile packages downloaded for macOS", func() {
		replicator := &fakeSinfReplicator{}
		out := appstore.DownloadOutput{
			DestinationPath: "app.ipa",
			Sinfs:           []appstore.Sinf{{Data: []byte("sinf")}},
		}

		err := replicateDownloadSinf(replicator, appstore.PlatformMacOS, out)

		Expect(err).ToNot(HaveOccurred())
		Expect(replicator.called).To(BeTrue())
		Expect(replicator.input).To(Equal(appstore.ReplicateSinfInput{
			PackagePath: out.DestinationPath,
			Sinfs:       out.Sinfs,
		}))
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

	DescribeTable("preserves the platform when automatic purchase retries the download", func(platform appstore.Platform) {
		store := &fakeDownloadAppStore{downloadErrors: []error{appstore.ErrLicenseRequired, nil}}
		previousDependencies := dependencies
		DeferCleanup(func() { dependencies = previousDependencies })
		dependencies.Logger = log.NewLogger(log.Args{})

		cmd := downloadCmdWithAppStore(func() appstore.AppStore { return store })
		cmd.SetArgs([]string{"--app-id", "42", "--platform", string(platform), "--purchase"})
		cmd.SetContext(context.WithValue(context.Background(), interactiveKey, false))

		Expect(cmd.Execute()).To(Succeed())
		Expect(store.accountInfoCalls).To(Equal(1))
		Expect(store.purchaseInputs).To(HaveLen(1))
		Expect(store.purchaseInputs[0].Platform).To(Equal(platform))
		Expect(store.purchaseInputs[0].App).To(Equal(appstore.App{ID: 42}))
		Expect(store.downloadInputs).To(HaveLen(2))
		Expect(store.downloadInputs[0].Platform).To(Equal(platform))
		Expect(store.downloadInputs[1].Platform).To(Equal(platform))
	},
		Entry("macOS", appstore.PlatformMacOS),
		Entry("watchOS", appstore.PlatformWatchOS),
	)

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
		Expect(store.accountInfoCalls).To(Equal(1))
		Expect(store.loginInputs).To(HaveLen(1))
		Expect(store.purchaseInputs).To(HaveLen(2))
		Expect(store.purchaseInputs[0].Platform).To(Equal(appstore.PlatformMacOS))
		Expect(store.purchaseInputs[1].Platform).To(Equal(appstore.PlatformMacOS))
		Expect(store.downloadInputs).To(HaveLen(2))
	})

	DescribeTable("reuses the account throughout download retries", func(downloadErrors []error, purchaseCount int) {
		account := appstore.Account{
			Email:         "user@example.com",
			Password:      "password",
			PasswordToken: "expired-token",
			StoreFront:    "143441",
		}
		refreshedAccount := account
		refreshedAccount.PasswordToken = "refreshed-token"
		store := &fakeDownloadAppStore{
			account:        account,
			loginAccount:   refreshedAccount,
			downloadErrors: downloadErrors,
		}
		previousDependencies := dependencies
		DeferCleanup(func() { dependencies = previousDependencies })
		dependencies.Logger = log.NewLogger(log.Args{})

		cmd := downloadCmdWithAppStore(func() appstore.AppStore { return store })
		cmd.SetArgs([]string{"--bundle-identifier", "com.example.app", "--purchase"})
		cmd.SetContext(context.WithValue(context.Background(), interactiveKey, false))

		Expect(cmd.Execute()).To(Succeed())
		Expect(store.accountInfoCalls).To(Equal(1))
		Expect(store.loginInputs).To(Equal([]appstore.LoginInput{{Email: account.Email, Password: account.Password}}))
		Expect(store.lookupInputs).To(HaveLen(len(downloadErrors)))
		Expect(store.lookupInputs[0].Account).To(Equal(account))
		Expect(store.downloadInputs).To(HaveLen(len(downloadErrors)))
		Expect(store.downloadInputs[0].Account).To(Equal(account))

		for _, input := range store.lookupInputs[1:] {
			Expect(input.Account).To(Equal(refreshedAccount))
		}

		for _, input := range store.downloadInputs[1:] {
			Expect(input.Account).To(Equal(refreshedAccount))
		}

		Expect(store.purchaseInputs).To(HaveLen(purchaseCount))

		for _, input := range store.purchaseInputs {
			Expect(input.Account).To(Equal(refreshedAccount))
		}
	},
		Entry("refreshes an expired token", []error{appstore.ErrPasswordTokenExpired, nil}, 0),
		Entry("retains the refreshed token for a subsequent purchase", []error{appstore.ErrPasswordTokenExpired, appstore.ErrLicenseRequired, nil}, 1),
	)

	It("returns an account read error before attempting a download", func() {
		store := &fakeDownloadAppStore{accountInfoError: errors.New("account unavailable")}
		cmd := downloadCmdWithAppStore(func() appstore.AppStore { return store })
		cmd.SetArgs([]string{"--app-id", "42"})

		Expect(cmd.Execute()).To(MatchError(store.accountInfoError))
		Expect(store.accountInfoCalls).To(Equal(1))
		Expect(store.loginInputs).To(BeEmpty())
		Expect(store.lookupInputs).To(BeEmpty())
		Expect(store.purchaseInputs).To(BeEmpty())
		Expect(store.downloadInputs).To(BeEmpty())
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
	account          appstore.Account
	accountInfoCalls int
	accountInfoError error
	downloadErrors   []error
	downloadInputs   []appstore.DownloadInput
	loginAccount     appstore.Account
	loginInputs      []appstore.LoginInput
	lookupInputs     []appstore.LookupInput
	lookupOutput     appstore.LookupOutput
	lookupError      error
	purchaseErrors   []error
	purchaseInputs   []appstore.PurchaseInput
}

func (f *fakeDownloadAppStore) Login(input appstore.LoginInput) (appstore.LoginOutput, error) {
	f.loginInputs = append(f.loginInputs, input)

	return appstore.LoginOutput{Account: f.loginAccount}, nil
}

func (f *fakeDownloadAppStore) AccountInfo() (appstore.AccountInfoOutput, error) {
	f.accountInfoCalls++

	return appstore.AccountInfoOutput{Account: f.account}, f.accountInfoError
}

func (*fakeDownloadAppStore) Revoke() error { return nil }

func (f *fakeDownloadAppStore) Lookup(input appstore.LookupInput) (appstore.LookupOutput, error) {
	f.lookupInputs = append(f.lookupInputs, input)

	return f.lookupOutput, f.lookupError
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
