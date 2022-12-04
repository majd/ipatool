package cmd

import (
	"github.com/99designs/keyring"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func searchCmd() *cobra.Command {
	var keychainPassphrase string
	var limit int64

	cmd := &cobra.Command{
		Use:   "search <term>",
		Short: "Search for iOS apps available on the App Store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appstore, err := newAppStore(cmd, keychainPassphrase)
			if err != nil {
				return errors.Wrap(err, "failed to create appstore client")
			}

			return appstore.Search(args[0], limit)
		},
	}

	cmd.Flags().Int64VarP(&limit, "limit", "l", 5, "maximum amount of search results to retrieve")

	if keyringBackendType() == keyring.FileBackend {
		cmd.PersistentFlags().StringVar(&keychainPassphrase, "keychain-passphrase", "", "passphrase for unlocking keychain")
	}

	return cmd
}
