package cmd

import (
	"fmt"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/spf13/cobra"
)

var setCmd = &cobra.Command{
	Use:   "set",
	Short: "Set configuration values",
}

var setWorkspaceCmd = &cobra.Command{
	Use:     "workspace <workspace-name>",
	Aliases: []string{"workspaces", "ws"},
	Short:   "Set the current workspace (ws)",
	Long: `Set the current workspace.

The workspace is stored in ~/.sgs/config.yaml and used for all subsequent commands.

Examples:
  sgs set workspace my-project`,
	Args: cobra.ExactArgs(1),
	Run:  runSetWorkspace,
}

var setModeCmd = &cobra.Command{
	Use:    "mode <prod|dev>",
	Short:  "Switch between production and development clusters",
	Long: `Switch between production and development clusters.

- prod: Use the production cluster (default)
- dev: Use the development cluster

This command is hidden and intended for internal use only.`,
	Args:   cobra.ExactArgs(1),
	Hidden: true,
	Run:    runSetMode,
}

func init() {
	setCmd.AddCommand(setWorkspaceCmd)
	setCmd.AddCommand(setModeCmd)
}

func runSetWorkspace(cmd *cobra.Command, args []string) {
	workspace := args[0]

	if err := client.SetWorkspace(workspace); err != nil {
		exitWithError("failed to set workspace", err)
	}

	fmt.Printf("Workspace set to: %s\n", workspace)
}

func runSetMode(cmd *cobra.Command, args []string) {
	mode := args[0]

	// Validate mode
	if mode != "prod" && mode != "dev" {
		exitWithError("mode must be either 'prod' or 'dev'", nil)
	}

	if err := client.SetMode(mode); err != nil {
		exitWithError("failed to set mode", err)
	}

	fmt.Printf("Mode set to %s\n", mode)
}
