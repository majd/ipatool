package cmd

import (
	"errors"
	"time"

	"github.com/avast/retry-go"
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/spf13/cobra"
)

// nolint:wrapcheck
func purchaseCmd() *cobra.Command {
	return purchaseCmdWithAppStore(func() appstore.AppStore { return dependencies.AppStore })
}

//nolint:wrapcheck
func purchaseCmdWithAppStore(appStore func() appstore.AppStore) *cobra.Command {
	var (
		bundleID      string
		platformValue string
	)

	cmd := &cobra.Command{
		Use:   "purchase",
		Short: "Obtain a license for the app from the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
			platform, err := appstore.ParsePlatform(platformValue)
			if err != nil {
				return err
			}

			var lastErr error
			var acc appstore.Account

			return retry.Do(func() error {
				store := appStore()
				infoResult, err := store.AccountInfo()
				if err != nil {
					return err
				}

				acc = infoResult.Account

				if errors.Is(lastErr, appstore.ErrPasswordTokenExpired) {
					loginResult, err := store.Login(appstore.LoginInput{
						Email:    acc.Email,
						Password: acc.Password,
					})
					if err != nil {
						return err
					}

					acc = loginResult.Account
				}

				lookupResult, err := store.Lookup(appstore.LookupInput{
					Account:  acc,
					BundleID: bundleID,
					Platform: platform,
				})
				if err != nil {
					return err
				}

				err = store.Purchase(appstore.PurchaseInput{
					Account:  acc,
					App:      lookupResult.App,
					Platform: platform,
				})
				if err != nil && !errors.Is(err, appstore.ErrLicenseAlreadyExists) {
					return err
				}

				dependencies.Logger.Log().
					Bool("alreadyOwned", errors.Is(err, appstore.ErrLicenseAlreadyExists)).
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

	cmd.Flags().StringVarP(&bundleID, "bundle-identifier", "b", "", "Bundle identifier of the target app (required)")
	cmd.Flags().StringVar(&platformValue, "platform", "", "Platform to purchase for: iphone (iOS), ipad (iPadOS), appletv (tvOS), visionos, or macos")
	_ = cmd.MarkFlagRequired("bundle-identifier")

	return cmd
}
