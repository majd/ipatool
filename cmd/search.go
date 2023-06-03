package cmd

import (
	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/spf13/cobra"
)

// nolint:wrapcheck
func searchCmd() *cobra.Command {
	var limit int64

	cmd := &cobra.Command{
		Use:   "search <term>",
		Short: "Search for iOS apps available on the App Store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			infoResult, err := dependencies.AppStore.AccountInfo()
			if err != nil {
				return err
			}

			output, err := dependencies.AppStore.Search(appstore.SearchInput{
				Account: infoResult.Account,
				Term:    args[0],
				Limit:   limit,
			})
			if err != nil {
				return err
			}

			dependencies.Logger.Log().
				Int("count", output.Count).
				Array("apps", appstore.Apps(output.Results)).
				Send()

			return nil
		},
	}

	cmd.Flags().Int64VarP(&limit, "limit", "l", 5, "maximum amount of search results to retrieve")

	return cmd
}
