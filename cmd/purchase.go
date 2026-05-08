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
func purchaseCmd() *cobra.Command {
	var (
		bundleID string
		country  string
	)

	cmd := &cobra.Command{
		Use:   "purchase",
		Short: "Obtain a license for the app from the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
			var lastErr error
			var acc appstore.Account

			return retry.Do(func() error {
				infoResult, err := dependencies.AppStore.AccountInfo()
				if err == nil {
					acc = infoResult.Account
				}

				if country == "" && acc.StoreFront == "" {
					country = "US"
				}

				if errors.Is(lastErr, appstore.ErrPasswordTokenExpired) {
					if acc.Email == "" {
						return errors.New("login is required to purchase apps; please use the \"auth login\" command")
					}

					bagOutput, err := dependencies.AppStore.Bag(appstore.BagInput{})
					if err != nil {
						return fmt.Errorf("failed to get bag: %w", err)
					}

					loginResult, err := dependencies.AppStore.Login(appstore.LoginInput{
						Email:    acc.Email,
						Password: acc.Password,
						Endpoint: bagOutput.AuthEndpoint,
					})
					if err != nil {
						return err
					}

					acc = loginResult.Account
				}

				lookupResult, err := dependencies.AppStore.Lookup(appstore.LookupInput{
					Account:     acc,
					BundleID:    bundleID,
					CountryCode: country,
				})
				if err != nil {
					return err
				}

				if acc.Email == "" {
					return errors.New("login is required to purchase apps; please use the \"auth login\" command")
				}

				err = dependencies.AppStore.Purchase(appstore.PurchaseInput{Account: acc, App: lookupResult.App})
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

	cmd.Flags().StringVarP(&bundleID, "bundle-identifier", "b", "", "Bundle identifier of the target iOS app (required)")
	cmd.Flags().StringVarP(&country, "country", "c", "", "Country code to use for lookup (e.g. US, GB); defaults to the account's country")
	_ = cmd.MarkFlagRequired("bundle-identifier")

	return cmd
}
