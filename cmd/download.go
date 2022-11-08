package cmd

import (
	"github.com/majd/ipatool/pkg/appstore"
	"github.com/spf13/cobra"
)

func downloadCmd() *cobra.Command {
	var bundleIdentifier string
	var countryCode string
	var deviceFamily string
	var outputPath string
	var acquireLicense bool

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download (encrypted) iOS app packages from the App Store",
		Run:   func(cmd *cobra.Command, args []string) {},
	}

	cmd.Flags().StringVarP(&bundleIdentifier, "bundle-identifier", "b", "", "The bundle identifier of the target iOS app (required)")
	cmd.Flags().StringVarP(&countryCode, "country", "c", "US", "The two-letter (ISO 3166-1 alpha-2) country code for the iTunes Store")
	cmd.Flags().StringVarP(&deviceFamily, "device-family", "d", appstore.DeviceFamilyPhone, "The device family to limit the search query to")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "The destination path of the downloaded app package")
	cmd.Flags().BoolVar(&acquireLicense, "purchase", false, "Obtain a license for the app if needed")

	_ = cmd.MarkFlagRequired("bundle-identifier")

	return cmd
}
