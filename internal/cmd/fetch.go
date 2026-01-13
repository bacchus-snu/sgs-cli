package cmd

import (
	"fmt"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/spf13/cobra"
)

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch or update the configuration",
	Long: `Fetch the configuration file from the server.

This command downloads the latest configuration and saves it to ~/.sgs/config.yaml.
Use this to update your configuration or to re-authenticate.

Examples:
  # Fetch/update configuration
  sgs fetch`,
	Args: cobra.NoArgs,
	Run:  runFetch,
}

func runFetch(cmd *cobra.Command, args []string) {
	fmt.Println("Fetching configuration...")

	if err := client.FetchConfig(); err != nil {
		exitWithError("failed to fetch configuration", err)
	}

	fmt.Println("Configuration updated successfully")
}
