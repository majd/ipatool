package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/avast/retry-go"
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/spf13/cobra"
)

func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with the App Store",
	}

	cmd.AddCommand(loginCmd())
	cmd.AddCommand(infoCmd())
	cmd.AddCommand(revokeCmd())

	return cmd
}

func loginCmd() *cobra.Command {
	var email, password, authCode string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login to the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
			interactive := cmd.Context().Value(interactiveKey).(bool)

			if email == "" && !interactive {
				return errors.New("email is required when not running in interactive mode; use the \"--email\" flag")
			}

			if email == "" && interactive {
				value, err := readPrompt("enter email: ", false)
				if err != nil {
					return fmt.Errorf("failed to read email: %w", err)
				}
				email = value
			}

			if password == "" && !interactive {
				return errors.New("password is required when not running in interactive mode; use the \"--password\" flag")
			}

			if password == "" && interactive {
				value, err := readPrompt("enter password: ", true)
				if err != nil {
					return fmt.Errorf("failed to read password: %w", err)
				}
				password = value
			}

			dependencies.Logger.Log().Msg("preparing authentication; the first login may take a few minutes")

			var lastErr error

			// nolint:wrapcheck
			return retry.Do(func() error {
				if errors.Is(lastErr, appstore.ErrAuthCodeRequired) && interactive {
					var err error
					authCode, err = readPrompt("enter 2FA code: ", false)
					if err != nil {
						return fmt.Errorf("failed to read auth code: %w", err)
					}
				}

				dependencies.Logger.Verbose().
					Str("email", email).
					Bool("authCodeProvided", authCode != "").
					Msg("logging in")

				output, err := dependencies.AppStore.Login(appstore.LoginInput{
					Email:    email,
					Password: password,
					AuthCode: authCode,
				})
				if err != nil {
					if errors.Is(err, appstore.ErrAuthCodeRequired) && !interactive {
						dependencies.Logger.Log().Msg("2FA code is required; run the command again and supply a code using the `--auth-code` flag")

						return nil
					}

					return err
				}

				dependencies.Logger.Log().
					Str("name", output.Account.Name).
					Str("email", output.Account.Email).
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

					return errors.Is(err, appstore.ErrAuthCodeRequired)
				}),
			)
		},
	}

	cmd.Flags().StringVarP(&email, "email", "e", "", "email address for the Apple ID (required)")
	cmd.Flags().StringVarP(&password, "password", "p", "", "password for the Apple ID (required)")
	cmd.Flags().StringVar(&authCode, "auth-code", "", "2FA code for the Apple ID")

	return cmd
}

// nolint:wrapcheck
func infoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show current account info",
		RunE: func(cmd *cobra.Command, args []string) error {
			output, err := dependencies.AppStore.AccountInfo()
			if err != nil {
				return err
			}

			dependencies.Logger.Log().
				Str("name", output.Account.Name).
				Str("email", output.Account.Email).
				Bool("success", true).
				Send()

			return nil
		},
	}
}

// nolint:wrapcheck
func revokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke",
		Short: "Revoke your App Store credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			err := dependencies.AppStore.Revoke()
			if err != nil {
				return err
			}

			dependencies.Logger.Log().Bool("success", true).Send()

			return nil
		},
	}
}
