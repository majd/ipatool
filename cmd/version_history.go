package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/avast/retry-go"
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/spf13/cobra"
)

// nolint:wrapcheck
func versionHistoryCmd() *cobra.Command {
	var (
		appID       int64
		bundleID    string
		maxCount    int
		oldestFirst bool
		allVersions bool
	)

	cmd := &cobra.Command{
		Use:   "version-history",
		Short: "Get version history for an iOS app from the App Store",
		RunE: func(cmd *cobra.Command, args []string) error {
			if appID == 0 && bundleID == "" {
				return errors.New("either the app ID or the bundle identifier must be specified")
			}

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

				app := appstore.App{ID: appID}
				if bundleID != "" {
					lookupResult, err := dependencies.AppStore.Lookup(appstore.LookupInput{Account: acc, BundleID: bundleID})
					if err != nil {
						return err
					}

					app = lookupResult.App
				}

				var appInfo *appstore.VersionHistoryInfo
				var headerPrinted bool
				displayedUpTo := -1
				resultBuffer := make(map[int]appstore.VersionDetails)

				appInfoCallback := func(info appstore.VersionHistoryInfo) {
					appInfo = &info

					fmt.Printf("App: %s", info.App.Name)
					if info.App.BundleID != "" {
						fmt.Printf(" (%s)", info.App.BundleID)
					}
					fmt.Printf("\nApp ID: %d\n", info.App.ID)
					fmt.Printf("Latest version: %s\n", info.LatestVersion)
					fmt.Printf("Total versions available: %d\n", len(info.VersionIdentifiers))

					orderDesc := "newest first"
					if oldestFirst {
						orderDesc = "oldest first"
					}

					if allVersions {
						fmt.Printf("\nFetching ALL version details (%s)...\n", orderDesc)
					} else {
						fmt.Printf("\nFetching version details (%s)...\n", orderDesc)
					}
				}

				progressCallback := func(index int, detail appstore.VersionDetails) {
					if !headerPrinted {
						fmt.Println("┌─────────────────┬─────────────────┐")
						fmt.Println("│ Version         │ Version ID      │")
						fmt.Println("├─────────────────┼─────────────────┤")
						headerPrinted = true
					}

					resultBuffer[index] = detail

					for i := displayedUpTo + 1; ; i++ {
						if bufferedDetail, exists := resultBuffer[i]; exists {
							version := bufferedDetail.VersionString
							if version == "" {
								version = "Unknown"
							}

							fmt.Printf("│ %-15s │ %-15s │\n",
								truncateString(version, 15),
								truncateString(bufferedDetail.VersionID, 15))

							displayedUpTo = i
							delete(resultBuffer, i)
						} else {
							break
						}
					}
				}

				out, err := dependencies.AppStore.VersionHistory(appstore.VersionHistoryInput{
					Account:          acc,
					App:              app,
					MaxCount:         maxCount,
					OldestFirst:      oldestFirst,
					AllVersions:      allVersions,
					AppInfoCallback:  appInfoCallback,
					ProgressCallback: progressCallback,
				})
				if err != nil {
					return err
				}

				if len(out.VersionDetails) > 0 && headerPrinted {
					fmt.Println("└─────────────────┴─────────────────┘")

					successCount := 0
					for _, detail := range out.VersionDetails {
						if detail.Success {
							successCount++
						}
					}

					orderDesc := "oldest first"
					if !oldestFirst {
						orderDesc = "newest first"
					}

					fmt.Printf("\nSummary: %d/%d versions retrieved successfully (%s)\n", successCount, len(out.VersionDetails), orderDesc)
				} else if appInfo != nil && len(out.VersionDetails) == 0 {
					fmt.Println("\nNo version details available.")
				} else if appInfo == nil {
					fmt.Printf("App: %s", out.VersionHistory.App.Name)
					if out.VersionHistory.App.BundleID != "" {
						fmt.Printf(" (%s)", out.VersionHistory.App.BundleID)
					}
					fmt.Printf("\nApp ID: %d\n", out.VersionHistory.App.ID)
					fmt.Printf("Latest version: %s\n", out.VersionHistory.LatestVersion)
					fmt.Printf("Total versions available: %d\n", len(out.VersionHistory.VersionIdentifiers))
					fmt.Println("\nNo version details available.")
				}

				dependencies.Logger.Log().
					Int64("appId", out.VersionHistory.App.ID).
					Str("bundleId", out.VersionHistory.App.BundleID).
					Str("name", out.VersionHistory.App.Name).
					Str("latestVersion", out.VersionHistory.LatestVersion).
					Int("totalVersions", len(out.VersionHistory.VersionIdentifiers)).
					Int("detailedVersions", len(out.VersionDetails)).
					Bool("oldestFirst", oldestFirst).
					Bool("allVersions", allVersions).
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

					return errors.Is(err, appstore.ErrPasswordTokenExpired)
				}),
			)
		},
	}

	cmd.Flags().Int64VarP(&appID, "app-id", "i", 0, "ID of the target iOS app (required)")
	cmd.Flags().StringVarP(&bundleID, "bundle-identifier", "b", "", "The bundle identifier of the target iOS app (overrides the app ID)")
	cmd.Flags().IntVarP(&maxCount, "max-versions", "m", 10, "Maximum number of recent versions to fetch details for (ignored when --all-versions is used)")
	cmd.Flags().BoolVar(&oldestFirst, "oldest-first", false, "Show oldest versions first instead of newest first")
	cmd.Flags().BoolVar(&allVersions, "all-versions", false, "Fetch details for all available versions (overrides --max-versions)")

	return cmd
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	if maxLen <= 3 {
		return s[:maxLen]
	}

	return s[:maxLen-3] + "..."
}
