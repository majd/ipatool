package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/avast/retry-go"
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/spf13/cobra"
)

// nolint:wrapcheck
func getVersionMetadataCmd() *cobra.Command {
	var (
		appID             int64
		bundleID          string
		externalVersionID string
		allVersions       bool
	)

	cmd := &cobra.Command{
		Use:   "get-version-metadata",
		Short: "Retrieves the metadata for a specific version of an app",
		RunE: func(cmd *cobra.Command, args []string) error {
			if appID == 0 && bundleID == "" {
				return errors.New("either the app ID or the bundle identifier must be specified")
			}

			if !allVersions && externalVersionID == "" {
				return errors.New("either the external version identifier must be specified or the --all-versions flag must be used")
			}

			if allVersions && externalVersionID != "" {
				return errors.New("the --all-versions flag cannot be used together with the --external-version-id flag")
			}

			var lastErr error
			var acc appstore.Account

			return retry.Do(func() error {
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

				if allVersions {
					versions, err := dependencies.AppStore.ListVersions(appstore.ListVersionsInput{Account: acc, App: app})
					if err != nil {
						return err
					}

					versionDetails := make([]map[string]interface{}, 0, len(versions.ExternalVersionIdentifiers))
					hasFailure := false
					failureCount := 0
					verboseMode := cmd.Flag("verbose").Value.String() == "true"

					for _, versionID := range versions.ExternalVersionIdentifiers {
						entry := map[string]interface{}{
							"externalVersionID": versionID,
						}

						meta, err := dependencies.AppStore.GetVersionMetadata(appstore.GetVersionMetadataInput{
							Account:   acc,
							App:       app,
							VersionID: versionID,
						})
						if err != nil {
							hasFailure = true
							failureCount++
							entry["success"] = false
							if verboseMode {
								entry["error"] = err.Error()
							}
						} else {
							entry["displayVersion"] = meta.DisplayVersion
							entry["success"] = true
						}

						versionDetails = append(versionDetails, entry)
					}

					dependencies.Logger.Log().
						Str("bundleID", app.BundleID).
						Interface("versions", versionDetails).
						Bool("success", !hasFailure).
						Send()

					if hasFailure {
						return fmt.Errorf("failed to resolve metadata for %d version(s)", failureCount)
					}

					return nil
				}

				out, err := dependencies.AppStore.GetVersionMetadata(appstore.GetVersionMetadataInput{
					Account:   acc,
					App:       app,
					VersionID: externalVersionID,
				})
				if err != nil {
					return err
				}

				dependencies.Logger.Log().
					Str("externalVersionID", externalVersionID).
					Str("displayVersion", out.DisplayVersion).
					Time("releaseDate", out.ReleaseDate).
					Bool("success", true).
					Send()

				return nil
			},
				retry.LastErrorOnly(true),
				retry.DelayType(retry.FixedDelay),
				retry.Delay(time.Millisecond),
				retry.Attempts(2),
				retry.RetryIf(func(err error) bool {
					lastErr = err

					return errors.Is(err, appstore.ErrPasswordTokenExpired)
				}),
			)
		},
	}

	cmd.Flags().Int64VarP(&appID, "app-id", "i", 0, "ID of the target iOS app (required)")
	cmd.Flags().StringVarP(&bundleID, "bundle-identifier", "b", "", "The bundle identifier of the target iOS app (overrides the app ID)")
	cmd.Flags().StringVar(&externalVersionID, "external-version-id", "", "External version identifier of the target iOS app")
	cmd.Flags().BoolVar(&allVersions, "all-versions", false, "Retrieve metadata for all available versions")

	return cmd
}
