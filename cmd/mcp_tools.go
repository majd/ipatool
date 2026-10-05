package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/majd/ipatool/v2/pkg/appstore"
)

type mcpSearchInput struct {
	Term     string `json:"term" jsonschema:"Search term"`
	Limit    *int64 `json:"limit,omitempty" jsonschema:"Maximum results; defaults to 5. visionOS supports up to 12."`
	Platform string `json:"platform,omitempty" jsonschema:"iphone (ios), ipad (ipados), appletv (tvos), visionos, or macos; defaults to iOS and iPadOS"`
}

type mcpSearchOutput struct {
	Term     string            `json:"term"`
	Limit    int64             `json:"limit"`
	Platform appstore.Platform `json:"platform"`
	Count    int               `json:"count"`
	Apps     []mcpApp          `json:"apps"`
}

type mcpAppInput struct {
	AppID    int64  `json:"app_id,omitempty" jsonschema:"Numeric app ID; required unless bundle_identifier is provided"`
	BundleID string `json:"bundle_identifier,omitempty" jsonschema:"Bundle identifier; overrides app_id when found"`
	Platform string `json:"platform,omitempty" jsonschema:"iphone (ios), ipad (ipados), appletv (tvos), visionos, or macos; uses the CLI default when omitted"`
}

type mcpDownloadInput struct {
	mcpAppInput
	Output            string `json:"output,omitempty" jsonschema:"Destination file or directory; defaults to the server's working directory"`
	ExternalVersionID string `json:"external_version_id,omitempty" jsonschema:"External version identifier; defaults to the latest version"`
	Purchase          bool   `json:"purchase,omitempty" jsonschema:"Acquire a free license if needed; defaults to false"`
}

type mcpDownloadOutput struct {
	mcpAppParameters
	App               mcpApp `json:"app"`
	Output            string `json:"output"`
	ExternalVersionID string `json:"external_version_id"`
	Purchase          bool   `json:"purchase"`
	Purchased         bool   `json:"purchased"`
	Success           bool   `json:"success"`
}

type mcpVersionsOutput struct {
	mcpAppParameters
	App                        mcpApp   `json:"app"`
	ExternalVersionIdentifiers []string `json:"externalVersionIdentifiers"`
	LatestExternalVersionID    string   `json:"latestExternalVersionID"`
	BundleID                   string   `json:"bundleID"`
	Success                    bool     `json:"success"`
}

type mcpPurchasesInput struct {
	Page       *int   `json:"page,omitempty" jsonschema:"Page number starting at 1; defaults to 1"`
	MaxResults *int   `json:"max_results,omitempty" jsonschema:"Apps per page from 1 to 100; defaults to 10"`
	Platform   string `json:"platform,omitempty" jsonschema:"iphone (ios), ipad (ipados), appletv (tvos), visionos, or macos; defaults to all platforms"`
}

type mcpPurchasesOutput struct {
	Count      int               `json:"count"`
	TotalCount int               `json:"totalCount"`
	Page       int               `json:"page"`
	MaxResults int               `json:"max_results"`
	Platform   appstore.Platform `json:"platform"`
	Apps       []mcpApp          `json:"apps"`
}

type mcpPurchaseOutput struct {
	mcpAppParameters
	App          mcpApp `json:"app"`
	AlreadyOwned bool   `json:"alreadyOwned"`
	Success      bool   `json:"success"`
}

// withMCPAccount refreshes expired tokens using the saved credentials, as the CLI does.
// Authentication and account details are never included in tool results.
func withMCPAccount[Out any](ctx context.Context, store appstore.AppStore, operation func(appstore.Account) (Out, error)) (Out, error) {
	var zero Out

	info, err := store.AccountInfo()
	if err != nil {
		return zero, fmt.Errorf("account unavailable; run 'ipatool auth login' before using MCP: %w", err)
	}

	output, err := operation(info.Account)
	if !errors.Is(err, appstore.ErrPasswordTokenExpired) {
		return output, err
	}

	if err := ctx.Err(); err != nil {
		return zero, fmt.Errorf("tool call canceled: %w", err)
	}

	login, err := store.Login(appstore.LoginInput{Email: info.Account.Email, Password: info.Account.Password})
	if err != nil {
		return zero, fmt.Errorf("session refresh failed; run 'ipatool auth login' outside MCP: %w", err)
	}

	output, err = operation(login.Account)
	if err != nil {
		return zero, fmt.Errorf("operation failed after session refresh: %w", err)
	}

	return output, nil
}

//nolint:wrapcheck
func (t *mcpTools) search(ctx context.Context, input mcpSearchInput) (mcpSearchOutput, error) {
	if strings.TrimSpace(input.Term) == "" {
		return mcpSearchOutput{}, errors.New("search term must not be empty")
	}

	limit := int64(5)
	if input.Limit != nil {
		limit = *input.Limit
	}

	if limit < 1 {
		return mcpSearchOutput{}, errors.New("limit must be greater than 0")
	}

	platform, err := appstore.ParsePlatform(input.Platform)
	if err != nil {
		return mcpSearchOutput{}, err
	}

	return withMCPAccount(ctx, t.store, func(acc appstore.Account) (mcpSearchOutput, error) {
		out, err := t.store.Search(appstore.SearchInput{Account: acc, Term: input.Term, Limit: limit, Platform: platform})
		if err != nil {
			return mcpSearchOutput{}, err
		}

		return mcpSearchOutput{
			Term: input.Term, Limit: limit, Platform: platform,
			Count: out.Count, Apps: t.appResults(acc, out.Results, platform),
		}, nil
	})
}

//nolint:wrapcheck
func (input mcpAppInput) validate() (appstore.Platform, error) {
	if input.AppID < 0 || (input.AppID == 0 && strings.TrimSpace(input.BundleID) == "") {
		return "", errors.New("a positive app ID or a bundle identifier must be specified")
	}

	return appstore.ParsePlatform(input.Platform)
}

//nolint:wrapcheck
func (t *mcpTools) resolveApp(acc appstore.Account, input mcpAppInput, platform appstore.Platform, allowDelisted bool) (appstore.App, error) {
	app := appstore.App{ID: input.AppID, BundleID: input.BundleID}
	if input.BundleID == "" {
		return t.completeApp(acc, app, platform), nil
	}

	out, err := t.store.Lookup(appstore.LookupInput{Account: acc, BundleID: input.BundleID, Platform: platform})
	if allowDelisted && input.AppID > 0 && errors.Is(err, appstore.ErrAppNotFound) {
		return app, nil
	}

	if err != nil {
		return appstore.App{}, err
	}

	// Preserve a known bundle identifier even if the catalog omits it.
	if out.App.BundleID == "" {
		out.App.BundleID = input.BundleID
	}

	return out.App, nil
}

//nolint:wrapcheck
func (t *mcpTools) download(ctx context.Context, input mcpDownloadInput) (mcpDownloadOutput, error) {
	platform, err := input.validate()
	if err != nil {
		return mcpDownloadOutput{}, err
	}

	purchased := false

	return withMCPAccount(ctx, t.store, func(acc appstore.Account) (mcpDownloadOutput, error) {
		app, err := t.resolveApp(acc, input.mcpAppInput, platform, true)
		if err != nil {
			return mcpDownloadOutput{}, err
		}

		downloadInput := appstore.DownloadInput{
			Context: ctx, Account: acc, App: app, OutputPath: input.Output,
			ExternalVersionID: input.ExternalVersionID, Platform: platform,
		}

		out, err := t.store.Download(downloadInput)
		if errors.Is(err, appstore.ErrLicenseRequired) && input.Purchase && !purchased {
			err = t.store.Purchase(appstore.PurchaseInput{Account: acc, App: app, Platform: platform})
			if err != nil && !errors.Is(err, appstore.ErrLicenseAlreadyExists) {
				return mcpDownloadOutput{}, err
			}

			purchased = true
			out, err = t.store.Download(downloadInput)
		}

		if err != nil {
			return mcpDownloadOutput{}, err
		}

		if err := replicateDownloadSinf(t.store, platform, out); err != nil {
			return mcpDownloadOutput{}, err
		}

		return mcpDownloadOutput{
			mcpAppParameters: appParameters(app, platform), App: newMCPApp(app),
			Output: out.DestinationPath, ExternalVersionID: input.ExternalVersionID,
			Purchase: input.Purchase, Purchased: purchased, Success: true,
		}, nil
	})
}

//nolint:wrapcheck
func (t *mcpTools) listVersions(ctx context.Context, input mcpAppInput) (mcpVersionsOutput, error) {
	platform, err := input.validate()
	if err != nil {
		return mcpVersionsOutput{}, err
	}

	return withMCPAccount(ctx, t.store, func(acc appstore.Account) (mcpVersionsOutput, error) {
		app, err := t.resolveApp(acc, input, platform, false)
		if err != nil {
			return mcpVersionsOutput{}, err
		}

		out, err := t.store.ListVersions(appstore.ListVersionsInput{Context: ctx, Account: acc, App: app, Platform: platform})
		if err != nil {
			return mcpVersionsOutput{}, err
		}

		return mcpVersionsOutput{
			mcpAppParameters: appParameters(app, platform), App: newMCPApp(app),
			ExternalVersionIdentifiers: append([]string{}, out.ExternalVersionIdentifiers...),
			LatestExternalVersionID:    out.LatestExternalVersionID, BundleID: app.BundleID, Success: true,
		}, nil
	})
}

//nolint:wrapcheck
func (t *mcpTools) listPurchases(ctx context.Context, input mcpPurchasesInput) (mcpPurchasesOutput, error) {
	page, limit := 1, appstore.DefaultOwnedAppsLimit
	if input.Page != nil {
		page = *input.Page
	}

	if input.MaxResults != nil {
		limit = *input.MaxResults
	}

	if page < 1 {
		return mcpPurchasesOutput{}, errors.New("page must be greater than 0")
	}

	if limit < 1 || limit > appstore.MaxOwnedAppsLimit {
		return mcpPurchasesOutput{}, fmt.Errorf("max results must be between 1 and %d", appstore.MaxOwnedAppsLimit)
	}

	platform, err := appstore.ParsePlatform(input.Platform)
	if err != nil {
		return mcpPurchasesOutput{}, err
	}

	return withMCPAccount(ctx, t.store, func(acc appstore.Account) (mcpPurchasesOutput, error) {
		out, err := t.store.OwnedApps(appstore.OwnedAppsInput{Account: acc, Page: page, Limit: limit, Platform: platform})
		if err != nil {
			return mcpPurchasesOutput{}, err
		}

		return mcpPurchasesOutput{
			Count: out.Count, TotalCount: out.TotalCount, Page: out.Page, MaxResults: limit, Platform: platform,
			Apps: t.appResults(acc, out.Results, platform),
		}, nil
	})
}

//nolint:wrapcheck
func (t *mcpTools) purchase(ctx context.Context, input mcpAppInput) (mcpPurchaseOutput, error) {
	platform, err := input.validate()
	if err != nil {
		return mcpPurchaseOutput{}, err
	}

	return withMCPAccount(ctx, t.store, func(acc appstore.Account) (mcpPurchaseOutput, error) {
		app, err := t.resolveApp(acc, input, platform, false)
		if err != nil {
			return mcpPurchaseOutput{}, err
		}

		err = t.store.Purchase(appstore.PurchaseInput{Account: acc, App: app, Platform: platform})
		if err != nil && !errors.Is(err, appstore.ErrLicenseAlreadyExists) {
			return mcpPurchaseOutput{}, err
		}

		return mcpPurchaseOutput{
			mcpAppParameters: appParameters(app, platform), App: newMCPApp(app),
			AlreadyOwned: errors.Is(err, appstore.ErrLicenseAlreadyExists), Success: true,
		}, nil
	})
}
