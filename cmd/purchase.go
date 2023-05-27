package cmd

import (
	"errors"
	"github.com/99designs/keyring"
	"github.com/avast/retry-go"
	"github.com/majd/ipatool/pkg/appstore"
	"github.com/spf13/cobra"
	"time"
)

func purchaseCmd() *cobra.Command {
	var bundleID string

	cmd := &cobra.Command{
		Use:   "purchase",
		Short: "Obtain a license for the app from the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
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

				lookupResult, err := dependencies.AppStore.Lookup(appstore.LookupInput{Account: acc, BundleID: bundleID})
				if err != nil {
					return err
				}

				err = dependencies.AppStore.Purchase(appstore.PurchaseInput{Account: acc, App: lookupResult.App})
				if err != nil {
					return err
				}

				dependencies.Logger.Log().Bool("success", true).Send()
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

	if keyringBackendType() == keyring.FileBackend {
		cmd.Flags().StringVar(&keychainPassphrase, "keychain-passphrase", "", "passphrase for unlocking keychain")
	}

	_ = cmd.MarkFlagRequired("bundle-identifier")

	return cmd
}
