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
	return downloadCmdWithAppStore(func() appstore.AppStore { return dependencies.AppStore })
}

//nolint:wrapcheck
func downloadCmdWithAppStore(appStore func() appstore.AppStore) *cobra.Command {
	var (
		acquireLicense    bool
		outputPath        string
		appID             int64
		bundleID          string
		externalVersionID string
		platformValue     string
	)

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download iOS, iPadOS, tvOS, visionOS, and macOS app packages from the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
			if appID == 0 && bundleID == "" {
				return errors.New("either the app ID or the bundle identifier must be specified")
			}

			platform, err := appstore.ParsePlatform(platformValue)
			if err != nil {
				return err
			}

			var lastErr error
			var acc appstore.Account
			purchaseRequired := false
			purchased := false

			return retry.Do(func() error {
				store := appStore()
				infoResult, err := store.AccountInfo()
				if err != nil {
					return err
				}

				acc = infoResult.Account

				if errors.Is(lastErr, appstore.ErrPasswordTokenExpired) {
					loginResult, err := store.Login(appstore.LoginInput{
						Email:    acc.Email,
						Password: acc.Password,
					})
					if err != nil {
						return err
					}

					acc = loginResult.Account
				}

				app := appstore.App{ID: appID}

				if bundleID != "" {
					lookupResult, err := store.Lookup(appstore.LookupInput{
						Account:  acc,
						BundleID: bundleID,
						Platform: platform,
					})
					if err != nil {
						return err
					}

					app = lookupResult.App
				}

				if errors.Is(lastErr, appstore.ErrLicenseRequired) {
					purchaseRequired = true
				}

				if purchaseRequired {
					err := store.Purchase(appstore.PurchaseInput{
						Account:  acc,
						App:      app,
						Platform: platform,
					})
					if err != nil && !errors.Is(err, appstore.ErrLicenseAlreadyExists) {
						return err
					}
					purchaseRequired = false
					purchased = true
					dependencies.Logger.Verbose().
						Bool("success", true).
						Msg("purchase")
				}

				interactive, _ := cmd.Context().Value(interactiveKey).(bool)
				var progress *progressbar.ProgressBar
				if interactive {
					progress = progressbar.NewOptions64(1,
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
				}

				out, err := store.Download(appstore.DownloadInput{
					Context:           cmd.Context(),
					Account:           acc,
					App:               app,
					OutputPath:        outputPath,
					Progress:          progress,
					ExternalVersionID: externalVersionID,
					Platform:          platform,
				})
				if err != nil {
					return err
				}

				if err := replicateDownloadSinf(store, platform, out); err != nil {
					return err
				}

				dependencies.Logger.Log().
					Str("output", out.DestinationPath).
					Bool("purchased", purchased).
					Bool("success", true).
					Send()

				return nil
			},
				retry.LastErrorOnly(true),
				retry.DelayType(retry.FixedDelay),
				retry.Delay(time.Millisecond),
				retry.Attempts(3),
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

	cmd.Flags().Int64VarP(&appID, "app-id", "i", 0, "ID of the target app (required)")
	cmd.Flags().StringVarP(&bundleID, "bundle-identifier", "b", "", "The bundle identifier of the target app (overrides the app ID)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "The destination path of the downloaded app package")
	cmd.Flags().StringVar(&externalVersionID, "external-version-id", "", "External version identifier of the target app (defaults to latest version when not specified)")
	cmd.Flags().StringVar(&platformValue, "platform", "", "Platform to download for: iphone (iOS), ipad (iPadOS), appletv (tvOS), visionos, or macos")
	cmd.Flags().BoolVar(&acquireLicense, "purchase", false, "Obtain a license for the app if needed")

	return cmd
}

type sinfReplicator interface {
	ReplicateSinf(input appstore.ReplicateSinfInput) error
}

//nolint:wrapcheck
func replicateDownloadSinf(store sinfReplicator, platform appstore.Platform, out appstore.DownloadOutput) error {
	if platform == appstore.PlatformMacOS && len(out.Sinfs) == 0 {
		return nil
	}

	return store.ReplicateSinf(appstore.ReplicateSinfInput{Sinfs: out.Sinfs, PackagePath: out.DestinationPath})
}
