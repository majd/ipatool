package cmd

import (
	"errors"
	"os"
	"time"

	"github.com/avast/retry-go"
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

// nolint:wrapcheck
func downloadCmd() *cobra.Command {
	var (
		acquireLicense bool
		outputPath     string
		bundleID       string
	)

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download (encrypted) iOS app packages from the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
			var lastErr error
			var acc appstore.Account

			return retry.Do(func() error {
				infoResult, err := dependencies.AppStore.AccountInfo()
				if err != nil {
					return err
				}

				acc = infoResult.Account

				if errors.Is(lastErr, appstore.ErrPasswordTokenExpired) {
					loginResult, err := dependencies.AppStore.Login(appstore.LoginInput{Email: acc.Email, Password: acc.Password})
					if err != nil {
						return err
					}

					acc = loginResult.Account
				}

				lookupResult, err := dependencies.AppStore.Lookup(appstore.LookupInput{Account: acc, BundleID: bundleID})
				if err != nil {
					return err
				}

				if errors.Is(lastErr, appstore.ErrLicenseRequired) {
					err := dependencies.AppStore.Purchase(appstore.PurchaseInput{Account: acc, App: lookupResult.App})
					if err != nil {
						return err
					}
				}

				progress := progressbar.NewOptions64(1,
					progressbar.OptionSetDescription("downloading"),
					progressbar.OptionSetWriter(os.Stdout),
					progressbar.OptionShowBytes(true),
					progressbar.OptionSetWidth(20),
					progressbar.OptionFullWidth(),
					progressbar.OptionThrottle(65*time.Millisecond),
					progressbar.OptionShowCount(),
					progressbar.OptionClearOnFinish(),
					progressbar.OptionSpinnerType(14),
					progressbar.OptionSetRenderBlankState(true),
					progressbar.OptionSetElapsedTime(false),
					progressbar.OptionSetPredictTime(false),
				)

				out, err := dependencies.AppStore.Download(appstore.DownloadInput{Account: acc, App: lookupResult.App, OutputPath: outputPath, Progress: progress})
				if err != nil {
					return err
				}

				err = dependencies.AppStore.ReplicateSinf(appstore.ReplicateSinfInput{Sinfs: out.Sinfs, PackagePath: out.DestinationPath})
				if err != nil {
					return err
				}

				dependencies.Logger.Log().
					Str("output", out.DestinationPath).
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

					if errors.Is(err, appstore.ErrPasswordTokenExpired) {
						return true
					}

					if errors.Is(err, appstore.ErrLicenseRequired) && acquireLicense {
						return true
					}

					return false
				}),
			)
		},
	}

	cmd.Flags().StringVarP(&bundleID, "bundle-identifier", "b", "", "The bundle identifier of the target iOS app (required)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "The destination path of the downloaded app package")
	cmd.Flags().BoolVar(&acquireLicense, "purchase", false, "Obtain a license for the app if needed")
	_ = cmd.MarkFlagRequired("bundle-identifier")

	return cmd
}
