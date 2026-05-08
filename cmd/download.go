package cmd

import (
	"errors"
	"fmt"
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
		acquireLicense    bool
		outputPath        string
		appID             int64
		bundleID          string
		externalVersionID string
		country           string
	)

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download (encrypted) iOS app packages from the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
			if appID == 0 && bundleID == "" {
				return errors.New("either the app ID or the bundle identifier must be specified")
			}

			var lastErr error
			var acc appstore.Account
			purchased := false

			return retry.Do(func() error {
				infoResult, err := dependencies.AppStore.AccountInfo()
				if err == nil {
					acc = infoResult.Account
				}

				if country == "" && acc.StoreFront == "" {
					country = "US"
				}

				if errors.Is(lastErr, appstore.ErrPasswordTokenExpired) {
					if acc.Email == "" {
						return errors.New("login is required to download apps; please use the \"auth login\" command")
					}

					bagOutput, err := dependencies.AppStore.Bag(appstore.BagInput{})
					if err != nil {
						return fmt.Errorf("failed to get bag: %w", err)
					}

					loginResult, err := dependencies.AppStore.Login(appstore.LoginInput{
						Email:    acc.Email,
						Password: acc.Password,
						Endpoint: bagOutput.AuthEndpoint,
					})
					if err != nil {
						return err
					}

					acc = loginResult.Account
				}

				app := appstore.App{ID: appID}
				if bundleID != "" {
					lookupResult, err := dependencies.AppStore.Lookup(appstore.LookupInput{
						Account:     acc,
						BundleID:    bundleID,
						CountryCode: country,
					})
					if err != nil {
						return err
					}

					app = lookupResult.App
				}

				if acc.Email == "" {
					return errors.New("login is required to download apps; please use the \"auth login\" command")
				}

				if errors.Is(lastErr, appstore.ErrLicenseRequired) {
					err := dependencies.AppStore.Purchase(appstore.PurchaseInput{Account: acc, App: app})
					if err != nil && !errors.Is(err, appstore.ErrLicenseAlreadyExists) {
						return err
					}
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

				out, err := dependencies.AppStore.Download(appstore.DownloadInput{
					Account: acc, App: app, OutputPath: outputPath, Progress: progress, ExternalVersionID: externalVersionID})
				if err != nil {
					if errors.Is(err, appstore.ErrLicenseRequired) && !acquireLicense {
						return fmt.Errorf("%w; please use the \"--purchase\" flag to obtain a license for this app", err)
					}
					return err
				}

				err = dependencies.AppStore.ReplicateSinf(appstore.ReplicateSinfInput{Sinfs: out.Sinfs, PackagePath: out.DestinationPath})
				if err != nil {
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

	cmd.Flags().Int64VarP(&appID, "app-id", "i", 0, "ID of the target iOS app (required)")
	cmd.Flags().StringVarP(&bundleID, "bundle-identifier", "b", "", "The bundle identifier of the target iOS app (overrides the app ID)")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "The destination path of the downloaded app package")
	cmd.Flags().StringVar(&externalVersionID, "external-version-id", "", "External version identifier of the target iOS app (defaults to latest version when not specified)")
	cmd.Flags().BoolVar(&acquireLicense, "purchase", false, "Obtain a license for the app if needed")
	cmd.Flags().StringVarP(&country, "country", "c", "", "Country code to use for lookup (e.g. US, GB); defaults to the account's country")

	return cmd
}
