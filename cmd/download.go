package cmd

import (
	"github.com/99designs/keyring"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func downloadCmd() *cobra.Command {
	var keychainPassphrase string
	var acquireLicense bool
	var outputPath string
	var bundleID string
	var skipExisting bool

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download (encrypted) iOS app packages from the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
			appstore, err := newAppStore(cmd, keychainPassphrase)
			if err != nil {
				return errors.Wrap(err, "failed to create appstore client")
			}

			return appstore.Download(bundleID, outputPath, acquireLicense, skipExisting)
		},
	}

	cmd.Flags().StringVarP(&bundleID, "bundle-identifier", "b", "", "The bundle identifier of the target iOS app (required)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "The destination path of the downloaded app package")
	cmd.Flags().BoolVar(&skipExisting, "skip-existing", false, "Skip downloading if destination path already exists")
	cmd.Flags().BoolVar(&acquireLicense, "purchase", false, "Obtain a license for the app if needed")

	if keyringBackendType() == keyring.FileBackend {
		cmd.Flags().StringVar(&keychainPassphrase, "keychain-passphrase", "", "passphrase for unlocking keychain")
	}

	_ = cmd.MarkFlagRequired("bundle-identifier")

	return cmd
}
