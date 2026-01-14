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
  all                - Describe all resources (nodes, volumes, sessions, workspaces)
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
  # Describe all resources
  sgs describe all

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
	case "all":
		getAll(ctx, k8sClient, true)
	case "nodes", "node":
		if name == "" {
			getNodes(ctx, k8sClient, true)
		} else {
			getNode(ctx, k8sClient, name, true)
		}
	case "volumes", "volume":
		if name == "" {
			getVolumes(ctx, k8sClient, true)
		} else {
			getVolume(ctx, k8sClient, name, true)
		}
	case "sessions", "session":
		if name == "" {
			getSessions(ctx, k8sClient, true)
		} else {
			getSession(ctx, k8sClient, name, true)
		}
	case "workspaces", "workspace":
		if name == "" {
			getWorkspaces(ctx, k8sClient, true)
		} else {
			getWorkspace(ctx, k8sClient, name, true)
		}
	case "me":
		getMe(true)
	default:
		exitWithError(fmt.Sprintf("unknown resource type: %s", resource), nil)
	}
}
