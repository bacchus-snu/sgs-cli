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
	Use:   "workspace <workspace-name>",
	Short: "Set the current workspace",
	Long: `Set the current workspace (Kubernetes namespace).

The workspace is stored in ~/.sgs/config.yaml and used for all subsequent commands.

Examples:
  # Set workspace
  sgs set workspace my-project`,
	Args: cobra.ExactArgs(1),
	Run:  runSetWorkspace,
}

func init() {
	setCmd.AddCommand(setWorkspaceCmd)
}

func runSetWorkspace(cmd *cobra.Command, args []string) {
	workspace := args[0]

	if err := client.SetWorkspace(workspace); err != nil {
		exitWithError("failed to set workspace", err)
	}

	fmt.Printf("Workspace set to: %s\n", workspace)
}
