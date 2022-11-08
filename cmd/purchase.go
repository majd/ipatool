package cmd

import (
	"github.com/majd/ipatool/pkg/appstore"
	"github.com/spf13/cobra"
)

func purchaseCmd() *cobra.Command {
	var bundleIdentifier string
	var countryCode string
	var deviceFamily string

	cmd := &cobra.Command{
		Use:   "purchase",
		Short: "Obtain a license for the app from the App Store",
		Run:   func(cmd *cobra.Command, args []string) {},
	}

	cmd.Flags().StringVarP(&bundleIdentifier, "bundle-identifier", "b", "", "The bundle identifier of the target iOS app (required)")
	cmd.Flags().StringVarP(&countryCode, "country", "c", "US", "The two-letter (ISO 3166-1 alpha-2) country code for the iTunes Store")
	cmd.Flags().StringVarP(&deviceFamily, "device-family", "d", appstore.DeviceFamilyPhone, "The device family to limit the search query to")

	_ = cmd.MarkFlagRequired("bundle-identifier")

	return cmd
}
