package cmd

import (
	"github.com/99designs/keyring"
	"github.com/majd/ipatool/pkg/appstore"
	"github.com/majd/ipatool/pkg/log"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var keychainPassphrase string

func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with the App Store",
	}

	if keyringBackendType() == keyring.FileBackend {
		cmd.PersistentFlags().StringVar(&keychainPassphrase, "keychain-passphrase", "", "passphrase for unlocking keychain")
	}

	cmd.AddCommand(loginCmd())
	cmd.AddCommand(infoCmd())
	cmd.AddCommand(revokeCmd())

	return cmd
}

func loginCmd() *cobra.Command {
	var email string
	var password string
	var authCode string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login to the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := newAppStore(cmd, keychainPassphrase)
			if err != nil {
				return errors.Wrap(err, "failed to create appstore client")
			}

			logger := cmd.Context().Value("logger").(log.Logger)
			out, err := store.Login(email, password, authCode)
			if err != nil {
				if err == appstore.ErrAuthCodeRequired {
					logger.Log().Msg("2FA code is required; run the command again and supply a code using the `--auth-code` flag")
					return nil
				}

				return err
			}

			logger.Log().
				Str("name", out.Name).
				Str("email", out.Email).
				Bool("success", true).
				Send()

			return nil
		},
	}

	cmd.Flags().StringVarP(&email, "email", "e", "", "email address for the Apple ID (required)")
	cmd.Flags().StringVarP(&password, "password", "p", "", "password for the Apple ID (required")
	cmd.Flags().StringVar(&authCode, "auth-code", "", "2FA code for the Apple ID")

	_ = cmd.MarkFlagRequired("email")

	return cmd
}

func infoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show current account info",
		RunE: func(cmd *cobra.Command, args []string) error {
			appstore, err := newAppStore(cmd, keychainPassphrase)
			if err != nil {
				return errors.Wrap(err, "failed to create appstore client")
			}

			out, err := appstore.Info()
			if err != nil {
				return err
			}

			logger := cmd.Context().Value("logger").(log.Logger)
			logger.Log().
				Str("name", out.Name).
				Str("email", out.Email).
				Bool("success", true).
				Send()

			return nil
		},
	}
}

func revokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke",
		Short: "Revoke your App Store credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			appstore, err := newAppStore(cmd, keychainPassphrase)
			if err != nil {
				return errors.Wrap(err, "failed to create appstore client")
			}

			err = appstore.Revoke()
			if err != nil {
				return err
			}

			logger := cmd.Context().Value("logger").(log.Logger)
			logger.Log().Bool("success", true).Send()

			return nil
		},
	}
}
