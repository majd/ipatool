package cmd

import (
	"errors"
	"reflect"

	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/spf13/cobra"
	"github.com/thediveo/enumflag/v2"
	"golang.org/x/net/context"
)

var version = "dev"

func rootCmd() *cobra.Command {
	var (
		verbose        bool
		nonInteractive bool
		format         OutputFormat
	)

	cmd := &cobra.Command{
		Use:           "ipatool",
		Short:         "A cli tool for interacting with Apple's ipa files",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       version,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			ctx := context.WithValue(context.Background(), "interactive", !nonInteractive)
			cmd.SetContext(ctx)
			initWithCommand(cmd)
		},
	}

	cmd.PersistentFlags().VarP(
		enumflag.New(&format, "format", map[OutputFormat][]string{
			OutputFormatText: {"text"},
			OutputFormatJSON: {"json"},
		}, enumflag.EnumCaseSensitive), "format", "", "sets output format for command; can be 'text', 'json'")
	cmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enables verbose logs")
	cmd.PersistentFlags().BoolVarP(&nonInteractive, "non-interactive", "", false, "run in non-interactive session")
	cmd.PersistentFlags().StringVar(&keychainPassphrase, "keychain-passphrase", "", "passphrase for unlocking keychain")

	cmd.AddCommand(authCmd())
	cmd.AddCommand(downloadCmd())
	cmd.AddCommand(purchaseCmd())
	cmd.AddCommand(searchCmd())

	return cmd
}

// Execute runs the program and returns the appropriate exit status code.
func Execute() int {
	cmd := rootCmd()
	err := cmd.Execute()

	if err != nil {
		if reflect.ValueOf(dependencies).IsZero() {
			initWithCommand(cmd)
		}

		var appstoreErr *appstore.Error
		if errors.As(err, &appstoreErr) {
			dependencies.Logger.Verbose().Stack().
				Err(err).
				Interface("metadata", appstoreErr.Metadata).
				Send()
		} else {
			dependencies.Logger.Verbose().Stack().Err(err).Send()
		}

		dependencies.Logger.Error().
			Err(err).
			Bool("success", false).
			Send()

		return 1
	}

	return 0
}
