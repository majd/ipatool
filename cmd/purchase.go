package cmd

import (
	"github.com/99designs/keyring"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func purchaseCmd() *cobra.Command {
	var keychainPassphrase string
	var bundleID string
	var appID int64

	cmd := &cobra.Command{
		Use:   "purchase",
		Short: "Obtain a license for the app from the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(bundleID, appID)
			if err != nil {
				return err
			}

			appstore, err := newAppStore(cmd, keychainPassphrase)
			if err != nil {
				return errors.Wrap(err, "failed to create appstore client")
			}

			return appstore.Purchase(id)
		},
	}

	cmd.Flags().StringVarP(&bundleID, "bundle-identifier", "b", "", "Bundle identifier of the target iOS app")
	cmd.Flags().Int64VarP(&appID, "app-id", "i", -1, "App ID of the target iOS app")

	if keyringBackendType() == keyring.FileBackend {
		cmd.Flags().StringVar(&keychainPassphrase, "keychain-passphrase", "", "passphrase for unlocking keychain")
	}

	return cmd
}
