package cmd

import (
	"errors"
	"time"

	"github.com/avast/retry-go"
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/spf13/cobra"
)

// nolint:wrapcheck
func ListVersionsCmd() *cobra.Command {
	var (
		appID         int64
		bundleID      string
		platformValue string
	)

	cmd := &cobra.Command{
		Use:   "list-versions",
		Short: "List the available versions of an App Store app",
		RunE: func(cmd *cobra.Command, args []string) error {
			if appID == 0 && bundleID == "" {
				return errors.New("either the app ID or the bundle identifier must be specified")
			}

			platform, err := appstore.ParsePlatform(platformValue)
			if err != nil {
				return err
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
					loginResult, err := dependencies.AppStore.Login(appstore.LoginInput{
						Email:    acc.Email,
						Password: acc.Password,
					})
					if err != nil {
						return err
					}

					acc = loginResult.Account
				}

				app := appstore.App{ID: appID}
				if bundleID != "" {
					lookupResult, err := dependencies.AppStore.Lookup(appstore.LookupInput{Account: acc, BundleID: bundleID, Platform: platform})
					if err != nil {
						return err
					}

					app = lookupResult.App
				}

				out, err := dependencies.AppStore.ListVersions(appstore.ListVersionsInput{Account: acc, App: app, Platform: platform})
				if err != nil {
					return err
				}

				dependencies.Logger.Log().
					Interface("externalVersionIdentifiers", out.ExternalVersionIdentifiers).
					Str("bundleID", app.BundleID).
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

	cmd.Flags().Int64VarP(&appID, "app-id", "i", 0, "ID of the target app (required)")
	cmd.Flags().StringVarP(&bundleID, "bundle-identifier", "b", "", "The bundle identifier of the target app (overrides the app ID)")

	cmd.Flags().StringVar(&platformValue, "platform", "", "Platform to list versions for: iphone (iOS), ipad (iPadOS), appletv (tvOS), visionos, or macos")

	return cmd
}
