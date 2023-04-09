package cmd

import (
	"github.com/99designs/keyring"
	"github.com/majd/ipatool/pkg/log"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func purchaseCmd() *cobra.Command {
	var keychainPassphrase string
	var bundleID string

	cmd := &cobra.Command{
		Use:   "purchase",
		Short: "Obtain a license for the app from the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
			appstore, err := newAppStore(cmd, keychainPassphrase)
			if err != nil {
				return errors.Wrap(err, "failed to create appstore client")
			}

			err = appstore.Purchase(bundleID)
			if err != nil {
				return err
			}

			logger := cmd.Context().Value("logger").(log.Logger)
			logger.Log().Bool("success", true).Send()

			return nil
		},
	}

	cmd.Flags().StringVarP(&bundleID, "bundle-identifier", "b", "", "Bundle identifier of the target iOS app (required)")

	if keyringBackendType() == keyring.FileBackend {
		cmd.Flags().StringVar(&keychainPassphrase, "keychain-passphrase", "", "passphrase for unlocking keychain")
	}

	_ = cmd.MarkFlagRequired("bundle-identifier")

	return cmd
}
