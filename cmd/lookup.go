package cmd

import (
	"github.com/99designs/keyring"
	"github.com/majd/ipatool/pkg/log"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func lookupCmd() *cobra.Command {
	var keychainPassphrase string
	var bundleID string
	var appID int64
	var countryCode string

	cmd := &cobra.Command{
		Use:   "lookup",
		Short: "Lookup information about a specific iOS app on the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(bundleID, appID)
			if err != nil {
				return err
			}

			appstore, err := newAppStore(cmd, keychainPassphrase)
			if err != nil {
				return errors.Wrap(err, "failed to create appstore client")
			}

			app, err := appstore.Lookup(id, countryCode)
			if err != nil {
				return errors.Wrap(err, "failed to lookup app")
			}

			logger := cmd.Context().Value("logger").(log.Logger)
			logger.Log().Interface("app", app).Bool("success", true).Send()
			return nil
		},
	}

	cmd.Flags().StringVarP(&bundleID, "bundle-identifier", "b", "", "Bundle identifier of the target iOS app (required)")
	cmd.Flags().Int64VarP(&appID, "app-id", "i", -1, "App ID of the target iOS app")
	cmd.Flags().StringVarP(&countryCode, "country-code", "c", "", "Country code for the target iOS app (required)")

	if keyringBackendType() == keyring.FileBackend {
		cmd.PersistentFlags().StringVar(&keychainPassphrase, "keychain-passphrase", "", "passphrase for unlocking keychain")
	}

	_ = cmd.MarkFlagRequired("country-code")

	return cmd
}
