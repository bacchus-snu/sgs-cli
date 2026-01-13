package cmd

import (
	"context"
	"fmt"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/spf13/cobra"
)

var describeCmd = &cobra.Command{
	Use:   "describe <resource> [name]",
	Short: "Show detailed information about resources",
	Long: `Show detailed information about a resource.

Resource types:
  nodes              - Describe all worker nodes with detailed info
  node <name>        - Describe a specific node
  volumes            - Describe all volumes with detailed info
  volume <name>      - Describe a specific volume
  sessions           - Describe all sessions with detailed info
  session <name>     - Describe a specific session
  workspaces         - Describe all accessible workspaces
  workspace [name]   - Describe current workspace or specific workspace
  me                 - Show your user information

Examples:
  # Describe all worker nodes
  sgs describe nodes

  # Describe a specific node
  sgs describe node ferrari

  # Describe all volumes in current workspace
  sgs describe volumes

  # Describe a specific volume
  sgs describe volume my-volume

  # Describe all sessions
  sgs describe sessions

  # Describe a specific session
  sgs describe session edit-my-volume

  # Describe all accessible workspaces
  sgs describe workspaces

  # Describe current workspace
  sgs describe workspace

  # Show your user info
  sgs describe me`,
	Args: cobra.RangeArgs(1, 2),
	Run:  runDescribe,
}

func runDescribe(cmd *cobra.Command, args []string) {
	ctx := context.Background()

	k8sClient, err := client.New()
	if err != nil {
		exitWithError("failed to create client", err)
	}

	resource := args[0]
	var name string
	if len(args) > 1 {
		name = args[1]
	}

	switch resource {
	case "nodes":
		getNodes(ctx, k8sClient, true)
	case "node":
		if name == "" {
			exitWithError("node name required", nil)
		}
		getNode(ctx, k8sClient, name, true)
	case "volumes":
		getVolumes(ctx, k8sClient, true)
	case "volume":
		if name == "" {
			exitWithError("volume name required", nil)
		}
		getVolume(ctx, k8sClient, name, true)
	case "sessions":
		getSessions(ctx, k8sClient, true)
	case "session":
		if name == "" {
			exitWithError("session name required", nil)
		}
		getSession(ctx, k8sClient, name, true)
	case "workspaces":
		getWorkspaces(ctx, k8sClient, true)
	case "workspace":
		getWorkspace(ctx, k8sClient, name, true)
	case "me":
		getMe(true)
	default:
		exitWithError(fmt.Sprintf("unknown resource type: %s", resource), nil)
	}
}
