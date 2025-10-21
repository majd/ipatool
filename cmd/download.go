package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/avast/retry-go"
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

// nolint:wrapcheck
func downloadCmd() *cobra.Command {
	var (
		acquireLicense    bool
		outputPath        string
		appID             int64
		bundleID          string
		externalVersionID string
		allVersions       bool
	)

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download (encrypted) iOS app packages from the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			verbose, _ := cmd.Flags().GetBool("verbose")

			if appID == 0 && bundleID == "" {
				return errors.New("either the app ID or the bundle identifier must be specified")
			}

			if allVersions && externalVersionID != "" {
				return errors.New("the --all-versions flag cannot be used together with the --external-version-id flag")
			}

			var lastErr error
			var acc appstore.Account
			purchased := false

			completed := map[string]bool{}
			resultsByVersion := map[string]map[string]interface{}{}
			displayCache := map[string]string{}

			err := retry.Do(func() error {
				infoResult, err := dependencies.AppStore.AccountInfo()
				if err != nil {
					return err
				}

				acc = infoResult.Account

				if errors.Is(lastErr, appstore.ErrPasswordTokenExpired) {
					loginResult, err := dependencies.AppStore.Login(appstore.LoginInput{Email: acc.Email, Password: acc.Password})
					if err != nil {
						return err
					}

					acc = loginResult.Account
				}

				app := appstore.App{ID: appID}
				if bundleID != "" {
					lookupResult, err := dependencies.AppStore.Lookup(appstore.LookupInput{Account: acc, BundleID: bundleID})
					if err != nil {
						return err
					}

					app = lookupResult.App
				}

				if errors.Is(lastErr, appstore.ErrLicenseRequired) {
					if err := dependencies.AppStore.Purchase(appstore.PurchaseInput{Account: acc, App: app}); err != nil {
						return err
					}

					purchased = true
					dependencies.Logger.Verbose().
						Bool("success", true).
						Msg("purchase")
				}

				interactive, _ := cmd.Context().Value("interactive").(bool)
				newProgress := func(description string) *progressbar.ProgressBar {
					if !interactive {
						return nil
					}

					return progressbar.NewOptions64(1,
						progressbar.OptionSetDescription(description),
						progressbar.OptionSetWriter(os.Stdout),
						progressbar.OptionShowBytes(true),
						progressbar.OptionSetWidth(20),
						progressbar.OptionFullWidth(),
						progressbar.OptionThrottle(65*time.Millisecond),
						progressbar.OptionShowCount(),
						progressbar.OptionClearOnFinish(),
						progressbar.OptionSpinnerType(14),
						progressbar.OptionSetRenderBlankState(true),
						progressbar.OptionSetElapsedTime(false),
						progressbar.OptionSetPredictTime(false),
					)
				}

				progressLabel := func(versionID, displayVersion string) string {
					switch {
					case displayVersion != "" && versionID != "":
						return fmt.Sprintf("downloading %s (%s)", displayVersion, versionID)
					case displayVersion != "":
						return fmt.Sprintf("downloading %s", displayVersion)
					case versionID != "":
						return fmt.Sprintf("downloading %s", versionID)
					default:
						return "downloading"
					}
				}

				getDisplayVersion := func(versionID string) (string, error) {
					if versionID == "" {
						return "", nil
					}

					if val, ok := displayCache[versionID]; ok {
						return val, nil
					}

					select {
					case <-ctx.Done():
						return "", ctx.Err()
					default:
					}

					meta, err := dependencies.AppStore.GetVersionMetadata(appstore.GetVersionMetadataInput{
						Account:   acc,
						App:       app,
						VersionID: versionID,
					})
					if err != nil {
						return "", err
					}

					displayCache[versionID] = meta.DisplayVersion

					return meta.DisplayVersion, nil
				}

				processVersion := func(versionID string) (map[string]interface{}, error) {
					entry := map[string]interface{}{
						"externalVersionID": versionID,
					}

					displayVersion := ""
					if versionID != "" {
						dv, err := getDisplayVersion(versionID)
						if err != nil {
							entry["metadataError"] = err.Error()
							if errors.Is(err, appstore.ErrPasswordTokenExpired) ||
								errors.Is(err, appstore.ErrLicenseRequired) ||
								errors.Is(err, context.Canceled) {
								entry["success"] = false
								entry["error"] = err.Error()

								return entry, err
							}
						} else {
							displayVersion = dv
							entry["displayVersion"] = dv
						}
					}

					if verbose {
						dependencies.Logger.Verbose().
							Str("bundleID", app.BundleID).
							Str("externalVersionID", versionID).
							Str("displayVersion", displayVersion).
							Msg("download started")
					}

					select {
					case <-ctx.Done():
						entry["success"] = false
						entry["error"] = ctx.Err().Error()
						
						return entry, ctx.Err()
					default:
					}

					progress := newProgress(progressLabel(versionID, displayVersion))
					if progress != nil {
						defer func() { _ = progress.Close() }()
					}

					out, err := dependencies.AppStore.Download(appstore.DownloadInput{
						Account:           acc,
						App:               app,
						OutputPath:        outputPath,
						Progress:          progress,
						ExternalVersionID: versionID,
					})
					if err != nil {
						entry["success"] = false
						entry["error"] = err.Error()

						if verbose {
							dependencies.Logger.Verbose().
								Str("bundleID", app.BundleID).
								Str("externalVersionID", versionID).
								Str("displayVersion", displayVersion).
								Err(err).
								Msg("download failed")
						}

						return entry, err
					}

					if err := dependencies.AppStore.ReplicateSinf(appstore.ReplicateSinfInput{
						Sinfs:       out.Sinfs,
						PackagePath: out.DestinationPath,
					}); err != nil {
						entry["success"] = false
						entry["error"] = err.Error()

						if verbose {
							dependencies.Logger.Verbose().
								Str("bundleID", app.BundleID).
								Str("externalVersionID", versionID).
								Str("displayVersion", displayVersion).
								Err(err).
								Msg("download failed")
						}

						return entry, err
					}

					entry["success"] = true
					entry["output"] = out.DestinationPath
					completed[versionID] = true

					if displayVersion == "" && out.DestinationPath != "" {
						base := filepath.Base(out.DestinationPath)
						base = strings.TrimSuffix(base, filepath.Ext(base))
						if base != "" {
							parts := strings.Split(base, "_")
							if len(parts) > 0 {
								displayVersion = parts[len(parts)-1]
								entry["displayVersion"] = displayVersion
							}
						}
					}

					if verbose {
						dependencies.Logger.Verbose().
							Str("bundleID", app.BundleID).
							Str("externalVersionID", versionID).
							Str("displayVersion", displayVersion).
							Msg("download completed")
					}

					return entry, nil
				}

				if allVersions {
					if outputPath != "" {
						info, err := dependencies.OS.Stat(outputPath)
						if err != nil {
							if !dependencies.OS.IsNotExist(err) {
								return err
							}

							return errors.New("when using --all-versions, the --output path must point to an existing directory")
						}

						if !info.IsDir() {
							return errors.New("when using --all-versions, the --output path must point to a directory")
						}
					}

					versions, err := dependencies.AppStore.ListVersions(appstore.ListVersionsInput{Account: acc, App: app})
					if err != nil {
						return err
					}

					if len(versions.ExternalVersionIdentifiers) == 0 {
						return errors.New("no versions available for download")
					}

					hasFailure := false
					failedSummaries := []string{}

					for _, versionID := range versions.ExternalVersionIdentifiers {
						if completed[versionID] {
							continue
						}

						select {
						case <-ctx.Done():
							return ctx.Err()
						default:
						}

						entry, err := processVersion(versionID)
						resultsByVersion[versionID] = entry

						if err != nil {
							hasFailure = true

							summary := versionID
							if display, ok := entry["displayVersion"].(string); ok && display != "" {
								summary = fmt.Sprintf("%s (%s)", display, versionID)
							}
							if errStr, ok := entry["error"].(string); ok && errStr != "" {
								summary = fmt.Sprintf("%s: %s", summary, errStr)
							}
							failedSummaries = append(failedSummaries, summary)

							if errors.Is(err, appstore.ErrPasswordTokenExpired) ||
								errors.Is(err, appstore.ErrLicenseRequired) ||
								errors.Is(err, context.Canceled) {
								return err
							}
						}
					}

					orderedResults := make([]map[string]interface{}, 0, len(versions.ExternalVersionIdentifiers))
					for _, versionID := range versions.ExternalVersionIdentifiers {
						if entry, ok := resultsByVersion[versionID]; ok {
							orderedResults = append(orderedResults, entry)
						}
					}

					dependencies.Logger.Log().
						Str("bundleID", app.BundleID).
						Interface("downloads", orderedResults).
						Bool("purchased", purchased).
						Bool("success", !hasFailure).
						Send()

					if hasFailure {
						return fmt.Errorf("failed to download %s", strings.Join(failedSummaries, "; "))
					}

					return nil
				}

				if !allVersions && outputPath != "" {
					if _, err := dependencies.OS.Stat(outputPath); err != nil {
						if dependencies.OS.IsNotExist(err) {
							dir := filepath.Dir(outputPath)
							if dir != "." {
								if mkErr := dependencies.OS.MkdirAll(dir, 0o755); mkErr != nil {
									return fmt.Errorf("failed to create output directory: %w", mkErr)
								}
							}
						} else {
							return err
						}
					}
				}

				entry, err := processVersion(externalVersionID)
				if err != nil {
					return err
				}

				output, _ := entry["output"].(string)

				dependencies.Logger.Log().
					Str("output", output).
					Bool("purchased", purchased).
					Bool("success", true).
					Send()

				return nil
			},
				retry.LastErrorOnly(true),
				retry.Attempts(4),
				retry.Delay(500*time.Millisecond),
				retry.MaxDelay(10*time.Second),
				retry.DelayType(retry.BackOffDelay),
				retry.RetryIf(func(err error) bool {
					lastErr = err

					if errors.Is(err, appstore.ErrPasswordTokenExpired) {
						return true
					}

					if errors.Is(err, appstore.ErrLicenseRequired) && acquireLicense {
						return true
					}

					return false
				}),
			)

			return err
		},
	}

	cmd.Flags().Int64VarP(&appID, "app-id", "i", 0, "ID of the target iOS app (required)")
	cmd.Flags().StringVarP(&bundleID, "bundle-identifier", "b", "", "The bundle identifier of the target iOS app (overrides the app ID)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "The destination path of the downloaded app package")
	cmd.Flags().StringVar(&externalVersionID, "external-version-id", "", "External version identifier of the target iOS app (defaults to latest version when not specified)")
	cmd.Flags().BoolVar(&allVersions, "all-versions", false, "Download all available versions of the target iOS app")
	cmd.Flags().BoolVar(&acquireLicense, "purchase", false, "Obtain a license for the app if needed")

	return cmd
}
