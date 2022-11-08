package cmd

import (
	"github.com/majd/ipatool/pkg/appstore"
	"github.com/spf13/cobra"
)

func searchCmd() *cobra.Command {
	var limit int64
	var countryCode string
	var deviceFamily string

	cmd := &cobra.Command{
		Use:   "search <term>",
		Short: "Search for iOS apps available on the App Store",
		Args:  cobra.ExactArgs(1),
		Run:   func(cmd *cobra.Command, args []string) {},
	}

	cmd.Flags().Int64VarP(&limit, "limit", "l", 5, "The maximum amount of search results to retrieve")
	cmd.Flags().StringVarP(&countryCode, "country", "c", "US", "The two-letter (ISO 3166-1 alpha-2) country code for the iTunes Store")
	cmd.Flags().StringVarP(&deviceFamily, "device-family", "d", appstore.DeviceFamilyPhone, "The device family to limit the search query to")

	return cmd
}
