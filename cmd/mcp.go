package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/majd/ipatool/v2/pkg/appstore"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

func mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve App Store tools over MCP using stdio",
		Long: "Serve App Store tools over the Model Context Protocol using stdio.\n" +
			"Authenticate with 'ipatool auth login' before using the tools.\n" +
			"Runs non-interactively; authentication tools are not exposed.",
		Args: cobra.NoArgs,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cmd.SetContext(context.WithValue(cmd.Context(), interactiveKey, false))
			initWithCommand(cmd)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			server := newMCPServer(dependencies.AppStore)
			if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
				return fmt.Errorf("MCP server failed: %w", err)
			}

			return nil
		},
	}
}

type mcpTools struct {
	store appstore.AppStore
	gate  chan struct{}
}

func newMCPServer(store appstore.AppStore) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "ipatool", Version: version}, &mcp.ServerOptions{
		Instructions: "Use the existing ipatool account. If authentication is required, ask the user to run ipatool auth login outside MCP. Purchases support free apps only.",
	})
	tools := &mcpTools{store: store, gate: make(chan struct{}, 1)}
	addMCPTool(server, tools, "search_apps", "Search for apps on the App Store.", true, tools.search)
	addMCPTool(server, tools, "download_app", "Download an app package to the local filesystem. Optionally acquire a free license if purchase is true.", false, tools.download)
	addMCPTool(server, tools, "list_app_versions", "List available external version identifiers for an app.", true, tools.listVersions)
	addMCPTool(server, tools, "get_version_metadata", "Retrieve the display version and release date for a specific external version identifier of an app.", true, tools.getVersionMetadata)
	addMCPTool(server, tools, "list_purchases", "List apps owned by the authenticated account, with pagination.", true, tools.listPurchases)
	addMCPTool(server, tools, "purchase_app", "Obtain a license for a free app. Paid apps are not supported.", false, tools.purchase)

	return server
}

func addMCPTool[In, Out any](server *mcp.Server, tools *mcpTools, name, description string, readOnly bool, handler func(context.Context, In) (Out, error)) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		var zero Out

		// The App Store client shares account state and caches across calls.
		select {
		case tools.gate <- struct{}{}:
			defer func() { <-tools.gate }()
		case <-ctx.Done():
			return nil, zero, fmt.Errorf("tool call canceled: %w", ctx.Err())
		}

		if err := ctx.Err(); err != nil {
			return nil, zero, fmt.Errorf("tool call canceled: %w", err)
		}

		output, err := handler(ctx, input)
		if err != nil {
			return nil, zero, fmt.Errorf("%s failed: %w", name, err)
		}

		return nil, output, nil
	})
}
