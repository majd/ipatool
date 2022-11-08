package cmd

import (
	"github.com/spf13/cobra"
)

func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authenticate with the App Store",
	}

	cmd.AddCommand(loginCmd())
	cmd.AddCommand(revokeCmd())

	return cmd
}

func loginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login to the App Store",
		Run:   func(cmd *cobra.Command, args []string) {},
	}

	return cmd
}

func revokeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke",
		Short: "Revoke your App Store credentials",
		Run:   func(cmd *cobra.Command, args []string) {},
	}

	return cmd
}
