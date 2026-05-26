package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/avast/retry-go"
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/majd/ipatool/v2/pkg/util"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with the App Store",
	}

	cmd.AddCommand(loginCmd())
	cmd.AddCommand(infoCmd())
	cmd.AddCommand(revokeCmd())
	cmd.AddCommand(listAccountsCmd())
	cmd.AddCommand(switchAccountCmd())

	return cmd
}

func loginCmd() *cobra.Command {
	promptForAuthCode := func() (string, error) {
		authCode, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read string: %w", err)
		}

		authCode = strings.Trim(authCode, "\n")
		authCode = strings.Trim(authCode, "\r")

		return authCode, nil
	}

	var email, password, authCode string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login to the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
			interactive := cmd.Context().Value(interactiveKey).(bool)

			if password == "" && !interactive {
				return errors.New("password is required when not running in interactive mode; use the \"--password\" flag")
			}

			if password == "" && interactive {
				dependencies.Logger.Log().Msg("enter password:")

				bytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				if err != nil {
					return fmt.Errorf("failed to read password: %w", err)
				}
				password = string(bytes)
			}

			var lastErr error

			// nolint:wrapcheck
			return retry.Do(func() error {
				if errors.Is(lastErr, appstore.ErrAuthCodeRequired) && interactive {
					dependencies.Logger.Log().Msg("enter 2FA code:")

					var err error
					authCode, err = promptForAuthCode()
					if err != nil {
						return fmt.Errorf("failed to read auth code: %w", err)
					}
				}

				dependencies.Logger.Verbose().
					Str("password", password).
					Str("email", email).
					Str("authCode", util.IfEmpty(authCode, "<nil>")).
					Msg("logging in")

				bag, err := dependencies.AppStore.Bag(appstore.BagInput{})
				if err != nil {
					return fmt.Errorf("failed to get bag: %w", err)
				}

				output, err := dependencies.AppStore.Login(appstore.LoginInput{
					Email:    email,
					Password: password,
					AuthCode: authCode,
					Endpoint: bag.AuthEndpoint,
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

	_ = cmd.MarkFlagRequired("email")

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

func listAccountsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List saved App Store accounts",
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := dependencies.AppStore.AccountsInfo()
			if err != nil {
				return err
			}

			for _, acc := range data.Accounts {
				dependencies.Logger.Log().
					Str("name", acc.Name).
					Str("email", acc.Email)
			}
			return nil
		},
	}

	return cmd
}

// IF provider with -e --email, then switch to that account
// if not provided, list accounts and prompt user to select one
func switchAccountCmd() *cobra.Command {
	var email string
	cmd := &cobra.Command{
		Use:   "switch",
		Short: "Switch to a different App Store account",
		RunE: func(cmd *cobra.Command, args []string) error {
			if email == "" {
				accounts, err := dependencies.AppStore.AccountsInfo()
				if err != nil {
					return errors.New("no saved accounts find, please login first")
				}
				if len(accounts.Accounts) == 0 {
					return errors.New("no saved accounts find, please login first")
				}

				for i, acc := range accounts.Accounts {
					fmt.Printf("[%d] %s <%s>\n", i+1, acc.Name, acc.Email)
				}

				fmt.Print("Select an account by number: ")
				var selection int
				_, err = fmt.Scanf("%d", &selection)
				if err != nil {
					return fmt.Errorf("failed to read selection: %w", err)
				}
				if selection < 1 || selection > len(accounts.Accounts) {
					return fmt.Errorf("invalid selection")
				}
				email = accounts.Accounts[selection-1].Email
				fmt.Printf("Switching to account: %s\n", email)
				_, err = dependencies.AppStore.SwitchAccount(email)
				return err
			}
			_, err := dependencies.AppStore.SwitchAccount(email)
			return err
		},
	}

	cmd.Flags().StringVarP(&email, "email", "e", "", "email address for the Apple ID")

	return cmd
}
