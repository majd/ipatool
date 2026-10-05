package cmd

import (
	"time"

	"github.com/majd/ipatool/v2/pkg/appstore"
)

// mcpApp keeps every metadata field in the MCP response, including zero values.
// The App Store's JSON representation is also used by other callers, so its
// omission rules are left unchanged.
type mcpApp struct {
	ID           int64               `json:"trackId"`
	BundleID     string              `json:"bundleId"`
	Name         string              `json:"trackName"`
	Version      string              `json:"version"`
	Price        float64             `json:"price"`
	PurchaseDate *time.Time          `json:"purchaseDate"`
	Platforms    []appstore.Platform `json:"platforms"`
}

func newMCPApp(app appstore.App) mcpApp {
	output := mcpApp{
		ID: app.ID, BundleID: app.BundleID, Name: app.Name, Version: app.Version, Price: app.Price,
		Platforms: append([]appstore.Platform{}, app.Platforms...),
	}
	if !app.PurchaseDate.IsZero() {
		output.PurchaseDate = &app.PurchaseDate
	}

	return output
}

// completeApp supplements sparse metadata without making catalog availability a
// prerequisite for operating on owned or delisted apps.
func (t *mcpTools) completeApp(acc appstore.Account, app appstore.App, platform appstore.Platform) appstore.App {
	knownPlatforms := len(app.Platforms) > 0 && app.Platforms[0] != appstore.PlatformUnknown
	if app.ID <= 0 || (app.BundleID != "" && app.Name != "" && app.Version != "" && knownPlatforms) {
		return app
	}

	// Purchase history can contain several platforms when no filter was supplied.
	// Use an app's known platform for its catalog lookup in that case.
	if platform == "" && len(app.Platforms) > 0 && app.Platforms[0] != appstore.PlatformUnknown {
		platform = app.Platforms[0]
	}

	out, err := t.store.Lookup(appstore.LookupInput{Account: acc, AppID: app.ID, Platform: platform})
	if err != nil || out.App.ID != app.ID {
		return app
	}

	if app.BundleID == "" {
		app.BundleID = out.App.BundleID
	}

	if app.Name == "" {
		app.Name = out.App.Name
	}

	if app.Version == "" {
		app.Version = out.App.Version
	}

	if len(out.App.Platforms) > 0 && (len(app.Platforms) == 0 || (len(app.Platforms) == 1 && app.Platforms[0] == appstore.PlatformUnknown)) {
		app.Platforms = out.App.Platforms
	}

	if app.Price == 0 {
		app.Price = out.App.Price
	}

	return app
}

func (t *mcpTools) appResults(acc appstore.Account, apps []appstore.App, platform appstore.Platform) []mcpApp {
	results := make([]mcpApp, 0, len(apps))
	for _, app := range apps {
		results = append(results, newMCPApp(t.completeApp(acc, app, platform)))
	}

	return results
}

type mcpAppParameters struct {
	AppID            int64             `json:"app_id"`
	BundleIdentifier string            `json:"bundle_identifier"`
	Platform         appstore.Platform `json:"platform"`
}

func appParameters(app appstore.App, platform appstore.Platform) mcpAppParameters {
	return mcpAppParameters{AppID: app.ID, BundleIdentifier: app.BundleID, Platform: platform}
}
