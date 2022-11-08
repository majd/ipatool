package cmd

import (
	"github.com/majd/ipatool/pkg/log"
	"github.com/spf13/cobra"
	"os"
)

func configureLogger(level string) error {
	logLevel, err := log.LevelFromString(level)
	if err != nil {
		return err
	}

	log.Logger = log.Output(log.NewWriter()).Level(logLevel)
	return nil
}

func rootCmd() *cobra.Command {
	var logLevel string

	cmd := &cobra.Command{
		Use:   "ipatool",
		Short: "A cli tool for interacting with Apple's ipa files",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return configureLogger(logLevel)
		},
	}

	cmd.PersistentFlags().StringVar(&logLevel, "log-level", log.InfoLevel, "The log level")

	cmd.AddCommand(authCmd())
	cmd.AddCommand(downloadCmd())
	cmd.AddCommand(purchaseCmd())
	cmd.AddCommand(searchCmd())

	return cmd
}

func Execute() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
