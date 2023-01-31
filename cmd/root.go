package cmd

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/thediveo/enumflag/v2"
	"golang.org/x/net/context"
)

func rootCmd() *cobra.Command {
	var verbose bool
	var nonInteractive bool
	var format OutputFormat

	cmd := &cobra.Command{
		Use:           "ipatool",
		Short:         "A cli tool for interacting with Apple's ipa files",
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.WithValue(context.Background(), "logger", newLogger(format, verbose))
			ctx = context.WithValue(ctx, "interactive", nonInteractive == false)
			cmd.SetContext(ctx)

			err := configureConfigDirectory()
			if err != nil {
				return errors.Wrap(err, "failed to configure config directory")
			}

			return nil
		},
	}

	cmd.PersistentFlags().VarP(
		enumflag.New(&format, "format", map[OutputFormat][]string{
			OutputFormatText: {"text"},
			OutputFormatJSON: {"json"},
		}, enumflag.EnumCaseSensitive), "format", "", "sets output format for command; can be 'text', 'json'")
	cmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enables verbose logs")
	cmd.PersistentFlags().BoolVarP(&nonInteractive, "non-interactive", "", false, "run in non-interactive session")

	cmd.AddCommand(authCmd())
	cmd.AddCommand(downloadCmd())
	cmd.AddCommand(purchaseCmd())
	cmd.AddCommand(searchCmd())
	cmd.AddCommand(lookupCmd())

	return cmd
}

// Execute runs the program and returns the approperiate exit status code.
func Execute() (exitCode int) {
	cmd := rootCmd()
	err := cmd.Execute()
	if err != nil {
		exitCode = 1

		logger := newLogger(OutputFormatText, false)
		outputFormat, parseErr := parseOutputFormat(cmd.Flag("format").Value.String())
		if parseErr != nil {
			logger.Error().Err(parseErr).Send()
			return
		}

		logger = newLogger(outputFormat, cmd.Flag("verbose").Value.String() == "true")

		logger.Verbose().Stack().Err(err).Send()
		logger.Error().
			Err(err).
			Bool("success", false).
			Send()
	}

	return
}
