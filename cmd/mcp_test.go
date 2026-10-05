package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MCP server", func() {
	var (
		ctx     context.Context
		store   *fakeMCPAppStore
		session *mcp.ClientSession
		account appstore.Account
		app     appstore.App
	)

	BeforeEach(func() {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		DeferCleanup(cancel)
		account = appstore.Account{Email: "private-email", Password: "private-password", PasswordToken: "private-token"}
		app = appstore.App{ID: 123, BundleID: "com.example.app", Name: "Example"}
		store = &fakeMCPAppStore{}
		store.accountInfo = func() (appstore.AccountInfoOutput, error) {
			return appstore.AccountInfoOutput{Account: account}, nil
		}
		store.lookup = func(appstore.LookupInput) (appstore.LookupOutput, error) {
			return appstore.LookupOutput{}, appstore.ErrAppNotFound
		}
		clientTransport, serverTransport := mcp.NewInMemoryTransports()
		serverSession, err := newMCPServer(store).Connect(ctx, serverTransport, nil)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(serverSession.Close()).To(Succeed()) })
		session, err = mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(session.Close()).To(Succeed()) })
	})

	call := func(name string, args map[string]any) *mcp.CallToolResult {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		Expect(err).NotTo(HaveOccurred())

		return result
	}

	It("registers the mcp subcommand", func() {
		command, _, err := rootCmd().Find([]string{"mcp"})
		Expect(err).NotTo(HaveOccurred())
		Expect(command.Name()).To(Equal("mcp"))
		Expect(command.Args(command, []string{"unexpected"})).NotTo(Succeed())
	})

	It("advertises exactly five tools and their schemas without auth functions", func() {
		result, err := session.ListTools(ctx, nil)
		Expect(err).NotTo(HaveOccurred())
		names := make([]string, 0, len(result.Tools))
		for _, tool := range result.Tools {
			names = append(names, tool.Name)
			Expect(tool.InputSchema).NotTo(BeNil())
			Expect(tool.OutputSchema).NotTo(BeNil())
			Expect(tool.Annotations.ReadOnlyHint).To(Equal(tool.Name != "download_app" && tool.Name != "purchase_app"))
		}
		Expect(names).To(ConsistOf("search_apps", "download_app", "list_app_versions", "list_purchases", "purchase_app"))
	})

	It("searches with CLI defaults and returns structured app data without credentials", func() {
		var input appstore.SearchInput

		store.search = func(in appstore.SearchInput) (appstore.SearchOutput, error) {
			input = in

			return appstore.SearchOutput{Count: 1, Results: []appstore.App{app}}, nil
		}
		result := call("search_apps", map[string]any{"term": "example"})
		Expect(result.IsError).To(BeFalse())
		Expect(input).To(Equal(appstore.SearchInput{Account: account, Term: "example", Limit: 5}))
		data, err := json.Marshal(result.StructuredContent)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(ContainSubstring("com.example.app"))
		Expect(string(data)).NotTo(ContainSubstring("private-"))
		Expect(result.Content).To(HaveLen(1))
		Expect(result.StructuredContent).To(HaveKeyWithValue("term", "example"))
		Expect(result.StructuredContent).To(HaveKeyWithValue("limit", float64(5)))
		Expect(result.StructuredContent).To(HaveKeyWithValue("platform", ""))
	})

	It("passes search platform aliases and limits to the App Store", func() {
		var input appstore.SearchInput

		store.search = func(in appstore.SearchInput) (appstore.SearchOutput, error) {
			input = in

			return appstore.SearchOutput{}, nil
		}
		Expect(call("search_apps", map[string]any{"term": "example", "platform": "tvos", "limit": 7}).IsError).To(BeFalse())
		Expect(input.Platform).To(Equal(appstore.PlatformAppleTV))
		Expect(input.Limit).To(Equal(int64(7)))
	})

	DescribeTable("rejects invalid inputs before accessing the account", func(name string, args map[string]any) {
		accountCalls := 0
		store.accountInfo = func() (appstore.AccountInfoOutput, error) {
			accountCalls++

			return appstore.AccountInfoOutput{}, errors.New("unexpected account access")
		}
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		Expect(err != nil || (result != nil && result.IsError)).To(BeTrue())
		Expect(accountCalls).To(BeZero())
	},
		Entry("missing search term", "search_apps", map[string]any{}),
		Entry("blank search term", "search_apps", map[string]any{"term": "  "}),
		Entry("invalid search limit", "search_apps", map[string]any{"term": "app", "limit": 0}),
		Entry("wrong argument type", "search_apps", map[string]any{"term": 123}),
		Entry("credentials are not arguments", "search_apps", map[string]any{"term": "app", "password": "secret"}),
		Entry("invalid platform", "search_apps", map[string]any{"term": "app", "platform": "unknown"}),
		Entry("missing download app", "download_app", map[string]any{}),
		Entry("negative app ID", "purchase_app", map[string]any{"app_id": -1}),
		Entry("missing versions app", "list_app_versions", map[string]any{}),
		Entry("invalid page", "list_purchases", map[string]any{"page": 0}),
		Entry("invalid page size", "list_purchases", map[string]any{"max_results": 101}),
	)

	It("returns actionable account errors and keeps serving requests", func() {
		store.accountInfo = func() (appstore.AccountInfoOutput, error) {
			return appstore.AccountInfoOutput{}, errors.New("account not found")
		}
		result := call("list_purchases", map[string]any{})
		Expect(result.IsError).To(BeTrue())
		Expect(result.Content[0].(*mcp.TextContent).Text).To(ContainSubstring("ipatool auth login"))
		_, err := session.ListTools(ctx, nil)
		Expect(err).NotTo(HaveOccurred())
	})

	DescribeTable("lists purchases with pagination", func(args map[string]any, page, limit int, platform appstore.Platform) {
		var input appstore.OwnedAppsInput

		store.ownedApps = func(in appstore.OwnedAppsInput) (appstore.OwnedAppsOutput, error) {
			input = in

			return appstore.OwnedAppsOutput{Count: 1, TotalCount: 11, Page: in.Page, Results: []appstore.App{app}}, nil
		}
		result := call("list_purchases", args)
		Expect(result.IsError).To(BeFalse())
		Expect(input).To(Equal(appstore.OwnedAppsInput{Account: account, Page: page, Limit: limit, Platform: platform}))
		Expect(result.StructuredContent).To(HaveKeyWithValue("totalCount", float64(11)))
		Expect(result.StructuredContent).To(HaveKeyWithValue("page", float64(page)))
		Expect(result.StructuredContent).To(HaveKeyWithValue("max_results", float64(limit)))
		Expect(result.StructuredContent).To(HaveKeyWithValue("platform", string(platform)))
	},
		Entry("defaults", map[string]any{}, 1, 10, appstore.Platform("")),
		Entry("explicit values", map[string]any{"page": 2, "max_results": 5, "platform": "macos"}, 2, 5, appstore.PlatformMacOS),
	)

	It("resolves bundle identifiers and lists versions", func() {
		var lookup appstore.LookupInput
		var input appstore.ListVersionsInput

		store.lookup = func(in appstore.LookupInput) (appstore.LookupOutput, error) {
			lookup = in

			return appstore.LookupOutput{App: app}, nil
		}
		store.listVersions = func(in appstore.ListVersionsInput) (appstore.ListVersionsOutput, error) {
			input = in

			return appstore.ListVersionsOutput{ExternalVersionIdentifiers: []string{"100", "200"}, LatestExternalVersionID: "200"}, nil
		}
		result := call("list_app_versions", map[string]any{"app_id": 999, "bundle_identifier": app.BundleID, "platform": "ipados"})
		Expect(result.IsError).To(BeFalse())
		Expect(lookup).To(Equal(appstore.LookupInput{Account: account, BundleID: app.BundleID, Platform: appstore.PlatformIPad}))
		Expect(input.App).To(Equal(app))
		Expect(input.Context).NotTo(BeNil())
		Expect(result.StructuredContent).To(HaveKeyWithValue("externalVersionIdentifiers", []any{"100", "200"}))
		Expect(result.StructuredContent).To(HaveKeyWithValue("app_id", float64(app.ID)))
		Expect(result.StructuredContent).To(HaveKeyWithValue("bundle_identifier", app.BundleID))
		Expect(result.StructuredContent).To(HaveKeyWithValue("platform", "ipad"))
	})

	DescribeTable("purchases a free license", func(purchaseErr error, alreadyOwned bool) {
		var input appstore.PurchaseInput

		store.purchase = func(in appstore.PurchaseInput) error {
			input = in

			return purchaseErr
		}
		result := call("purchase_app", map[string]any{"app_id": 123, "platform": "visionos"})
		Expect(result.IsError).To(BeFalse())
		Expect(input).To(Equal(appstore.PurchaseInput{Account: account, App: appstore.App{ID: 123}, Platform: appstore.PlatformVisionOS}))
		Expect(result.StructuredContent).To(HaveKeyWithValue("alreadyOwned", alreadyOwned))
	},
		Entry("new license", nil, false),
		Entry("existing license", appstore.ErrLicenseAlreadyExists, true),
	)

	It("downloads a selected version and finalizes the package without a progress bar", func() {
		var input appstore.DownloadInput
		var sinfInput appstore.ReplicateSinfInput

		store.download = func(in appstore.DownloadInput) (appstore.DownloadOutput, error) {
			input = in

			return appstore.DownloadOutput{DestinationPath: "/tmp/example.ipa"}, nil
		}
		store.replicateSinf = func(in appstore.ReplicateSinfInput) error {
			sinfInput = in

			return nil
		}
		result := call("download_app", map[string]any{"app_id": 123, "output": "/tmp/example.ipa", "external_version_id": "200", "platform": "ios"})
		Expect(result.IsError).To(BeFalse())
		Expect(input.App.ID).To(Equal(int64(123)))
		Expect(input.OutputPath).To(Equal("/tmp/example.ipa"))
		Expect(input.ExternalVersionID).To(Equal("200"))
		Expect(input.Platform).To(Equal(appstore.PlatformIPhone))
		Expect(input.Context).NotTo(BeNil())
		Expect(input.Progress).To(BeNil())
		Expect(sinfInput.PackagePath).To(Equal(input.OutputPath))
		Expect(result.StructuredContent).To(HaveKeyWithValue("output", input.OutputPath))
		Expect(result.StructuredContent).To(HaveKeyWithValue("external_version_id", "200"))
		Expect(result.StructuredContent).To(HaveKeyWithValue("purchase", false))
	})

	It("downloads delisted macOS apps by ID without attempting IPA finalization", func() {
		var input appstore.DownloadInput

		store.lookup = func(appstore.LookupInput) (appstore.LookupOutput, error) {
			return appstore.LookupOutput{}, appstore.ErrAppNotFound
		}
		store.download = func(in appstore.DownloadInput) (appstore.DownloadOutput, error) {
			input = in

			return appstore.DownloadOutput{DestinationPath: "/tmp/example.pkg"}, nil
		}
		result := call("download_app", map[string]any{"app_id": 123, "bundle_identifier": app.BundleID, "platform": "macos"})
		Expect(result.IsError).To(BeFalse())
		Expect(input.App.ID).To(Equal(int64(123)))
	})

	DescribeTable("only acquires a license during download when requested", func(purchase bool) {
		downloads, purchases := 0, 0
		store.download = func(appstore.DownloadInput) (appstore.DownloadOutput, error) {
			downloads++
			if downloads == 1 {
				return appstore.DownloadOutput{}, appstore.ErrLicenseRequired
			}

			return appstore.DownloadOutput{DestinationPath: "/tmp/example.pkg"}, nil
		}
		store.purchase = func(appstore.PurchaseInput) error {
			purchases++

			return nil
		}
		result := call("download_app", map[string]any{"app_id": 123, "platform": "macos", "purchase": purchase})
		Expect(result.IsError).To(Equal(!purchase))
		if purchase {
			Expect(purchases).To(Equal(1))
			Expect(downloads).To(Equal(2))
			Expect(result.StructuredContent).To(HaveKeyWithValue("purchased", true))
		} else {
			Expect(purchases).To(BeZero())
			Expect(downloads).To(Equal(1))
		}
	}, Entry("explicit opt in", true), Entry("no opt in", false))

	It("refreshes an expired session using saved credentials", func() {
		var login appstore.LoginInput
		var accounts []appstore.Account

		refreshed := appstore.Account{PasswordToken: "refreshed-private-token"}
		store.purchase = func(in appstore.PurchaseInput) error {
			accounts = append(accounts, in.Account)
			if len(accounts) == 1 {
				return appstore.ErrPasswordTokenExpired
			}

			return nil
		}
		store.login = func(in appstore.LoginInput) (appstore.LoginOutput, error) {
			login = in

			return appstore.LoginOutput{Account: refreshed}, nil
		}
		Expect(call("purchase_app", map[string]any{"app_id": 123}).IsError).To(BeFalse())
		Expect(login).To(Equal(appstore.LoginInput{Email: account.Email, Password: account.Password}))
		Expect(accounts).To(Equal([]appstore.Account{account, refreshed}))
	})

	It("reports two-factor requirements as tool errors without requesting credentials", func() {
		store.purchase = func(appstore.PurchaseInput) error {
			return appstore.ErrPasswordTokenExpired
		}
		store.login = func(appstore.LoginInput) (appstore.LoginOutput, error) {
			return appstore.LoginOutput{}, appstore.ErrAuthCodeRequired
		}
		result := call("purchase_app", map[string]any{"app_id": 123})
		Expect(result.IsError).To(BeTrue())
		Expect(result.Content[0].(*mcp.TextContent).Text).To(ContainSubstring("ipatool auth login"))
	})

	It("does not expose App Store error metadata", func() {
		store.search = func(appstore.SearchInput) (appstore.SearchOutput, error) {
			return appstore.SearchOutput{}, appstore.NewErrorWithMetadata(errors.New("request failed"), account)
		}
		result := call("search_apps", map[string]any{"term": "example"})
		Expect(result.IsError).To(BeTrue())
		data, err := json.Marshal(result)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(ContainSubstring("request failed"))
		Expect(string(data)).NotTo(ContainSubstring("private-"))
	})

	It("reports package finalization failures as tool errors", func() {
		store.download = func(appstore.DownloadInput) (appstore.DownloadOutput, error) {
			return appstore.DownloadOutput{DestinationPath: "/tmp/example.ipa"}, nil
		}
		store.replicateSinf = func(appstore.ReplicateSinfInput) error {
			return errors.New("finalization failed")
		}
		result := call("download_app", map[string]any{"app_id": 123})
		Expect(result.IsError).To(BeTrue())
		Expect(result.Content[0].(*mcp.TextContent).Text).To(ContainSubstring("finalization failed"))
	})

	DescribeTable("returns every parameter and app field even when metadata is unavailable", func(name string, args map[string]any, expectedKeys []string) {
		partial := appstore.App{ID: 123}
		store.search = func(appstore.SearchInput) (appstore.SearchOutput, error) {
			return appstore.SearchOutput{Count: 1, Results: []appstore.App{partial}}, nil
		}
		store.ownedApps = func(appstore.OwnedAppsInput) (appstore.OwnedAppsOutput, error) {
			return appstore.OwnedAppsOutput{Count: 1, TotalCount: 1, Page: 1, Results: []appstore.App{partial}}, nil
		}
		store.listVersions = func(appstore.ListVersionsInput) (appstore.ListVersionsOutput, error) {
			return appstore.ListVersionsOutput{ExternalVersionIdentifiers: []string{"200"}}, nil
		}
		store.purchase = func(appstore.PurchaseInput) error { return nil }
		store.download = func(appstore.DownloadInput) (appstore.DownloadOutput, error) {
			return appstore.DownloadOutput{DestinationPath: "/tmp/example.ipa"}, nil
		}
		store.replicateSinf = func(appstore.ReplicateSinfInput) error { return nil }
		result := call(name, args)
		Expect(result.IsError).To(BeFalse())
		output := result.StructuredContent.(map[string]any)
		for _, key := range expectedKeys {
			Expect(output).To(HaveKey(key))
		}

		metadata := output["app"]
		if metadata == nil {
			metadata = output["apps"].([]any)[0]
		}
		Expect(metadata).To(Equal(map[string]any{
			"trackId": float64(123), "bundleId": "", "trackName": "", "version": "",
			"price": float64(0), "purchaseDate": nil, "platforms": []any{},
		}))
		textJSON, err := json.Marshal(result.StructuredContent)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Content[0].(*mcp.TextContent).Text).To(MatchJSON(string(textJSON)))
	},
		Entry("search", "search_apps", map[string]any{"term": "example"}, []string{"term", "limit", "platform", "count", "apps"}),
		Entry("purchases", "list_purchases", map[string]any{}, []string{"page", "max_results", "platform", "count", "totalCount", "apps"}),
		Entry("versions", "list_app_versions", map[string]any{"app_id": 123}, []string{"app_id", "bundle_identifier", "platform", "bundleID", "app", "externalVersionIdentifiers", "latestExternalVersionID", "success"}),
		Entry("purchase", "purchase_app", map[string]any{"app_id": 123}, []string{"app_id", "bundle_identifier", "platform", "app", "alreadyOwned", "success"}),
		Entry("download", "download_app", map[string]any{"app_id": 123}, []string{"app_id", "bundle_identifier", "platform", "app", "output", "external_version_id", "purchase", "purchased", "success"}),
	)

	DescribeTable("fills missing bundle identifiers in app lists without losing purchase metadata", func(name string, args map[string]any) {
		purchasedAt := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
		partial := appstore.App{ID: 123, Name: "Original name", PurchaseDate: purchasedAt, Platforms: []appstore.Platform{appstore.PlatformMacOS}}
		var lookup appstore.LookupInput

		store.lookup = func(in appstore.LookupInput) (appstore.LookupOutput, error) {
			lookup = in

			return appstore.LookupOutput{App: appstore.App{ID: 123, BundleID: app.BundleID, Name: "Catalog name", Version: "2.0", Price: 4.99}}, nil
		}
		store.search = func(appstore.SearchInput) (appstore.SearchOutput, error) {
			return appstore.SearchOutput{Count: 1, Results: []appstore.App{partial}}, nil
		}
		store.ownedApps = func(appstore.OwnedAppsInput) (appstore.OwnedAppsOutput, error) {
			return appstore.OwnedAppsOutput{Count: 1, TotalCount: 1, Page: 1, Results: []appstore.App{partial}}, nil
		}
		result := call(name, args)
		Expect(result.IsError).To(BeFalse())
		Expect(lookup).To(Equal(appstore.LookupInput{Account: account, AppID: 123, Platform: appstore.PlatformMacOS}))
		metadata := result.StructuredContent.(map[string]any)["apps"].([]any)[0]
		Expect(metadata).To(Equal(map[string]any{
			"trackId": float64(123), "bundleId": app.BundleID, "trackName": "Original name", "version": "2.0",
			"price": 4.99, "purchaseDate": purchasedAt.Format(time.RFC3339), "platforms": []any{"macos"},
		}))
	},
		Entry("search", "search_apps", map[string]any{"term": "example"}),
		Entry("purchases", "list_purchases", map[string]any{}),
	)

	DescribeTable("resolves bundle identifiers for numeric app IDs", func(name string) {
		var lookup appstore.LookupInput

		store.lookup = func(in appstore.LookupInput) (appstore.LookupOutput, error) {
			lookup = in

			return appstore.LookupOutput{App: app}, nil
		}
		store.purchase = func(appstore.PurchaseInput) error { return nil }
		store.listVersions = func(appstore.ListVersionsInput) (appstore.ListVersionsOutput, error) {
			return appstore.ListVersionsOutput{}, nil
		}
		store.download = func(appstore.DownloadInput) (appstore.DownloadOutput, error) {
			return appstore.DownloadOutput{DestinationPath: "/tmp/example.ipa"}, nil
		}
		store.replicateSinf = func(appstore.ReplicateSinfInput) error { return nil }
		result := call(name, map[string]any{"app_id": 123})
		Expect(result.IsError).To(BeFalse())
		Expect(lookup.AppID).To(Equal(int64(123)))
		Expect(result.StructuredContent).To(HaveKeyWithValue("app_id", float64(123)))
		Expect(result.StructuredContent).To(HaveKeyWithValue("bundle_identifier", app.BundleID))
		Expect(result.StructuredContent.(map[string]any)["app"]).To(HaveKeyWithValue("bundleId", app.BundleID))
		if name == "list_app_versions" {
			Expect(result.StructuredContent).To(HaveKeyWithValue("bundleID", app.BundleID))
			Expect(result.StructuredContent).To(HaveKeyWithValue("externalVersionIdentifiers", []any{}))
		}
	}, Entry("purchase", "purchase_app"), Entry("download", "download_app"), Entry("versions", "list_app_versions"))
})

type fakeMCPAppStore struct {
	appstore.AppStore
	accountInfo   func() (appstore.AccountInfoOutput, error)
	search        func(appstore.SearchInput) (appstore.SearchOutput, error)
	lookup        func(appstore.LookupInput) (appstore.LookupOutput, error)
	ownedApps     func(appstore.OwnedAppsInput) (appstore.OwnedAppsOutput, error)
	listVersions  func(appstore.ListVersionsInput) (appstore.ListVersionsOutput, error)
	purchase      func(appstore.PurchaseInput) error
	download      func(appstore.DownloadInput) (appstore.DownloadOutput, error)
	replicateSinf func(appstore.ReplicateSinfInput) error
	login         func(appstore.LoginInput) (appstore.LoginOutput, error)
}

func (s *fakeMCPAppStore) AccountInfo() (appstore.AccountInfoOutput, error) {
	return s.accountInfo()
}

func (s *fakeMCPAppStore) Search(input appstore.SearchInput) (appstore.SearchOutput, error) {
	return s.search(input)
}

func (s *fakeMCPAppStore) Lookup(input appstore.LookupInput) (appstore.LookupOutput, error) {
	return s.lookup(input)
}

func (s *fakeMCPAppStore) OwnedApps(input appstore.OwnedAppsInput) (appstore.OwnedAppsOutput, error) {
	return s.ownedApps(input)
}

func (s *fakeMCPAppStore) ListVersions(input appstore.ListVersionsInput) (appstore.ListVersionsOutput, error) {
	return s.listVersions(input)
}

func (s *fakeMCPAppStore) Purchase(input appstore.PurchaseInput) error {
	return s.purchase(input)
}

func (s *fakeMCPAppStore) Download(input appstore.DownloadInput) (appstore.DownloadOutput, error) {
	return s.download(input)
}

func (s *fakeMCPAppStore) ReplicateSinf(input appstore.ReplicateSinfInput) error {
	return s.replicateSinf(input)
}

func (s *fakeMCPAppStore) Login(input appstore.LoginInput) (appstore.LoginOutput, error) {
	return s.login(input)
}
