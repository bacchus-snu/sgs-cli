package cmd

import (
	"context"
	"fmt"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/spf13/cobra"
)

var describeCmd = &cobra.Command{
	Use:     "describe <resource> [name]",
	Aliases: []string{"des", "desc"},
	Short:   "Show detailed information about resources (des, desc)",
	Long: `Show detailed information about a resource.

Resource types (aliases):
  all                 Describe all resources
  me                  Show your user information
  node (no)           Worker nodes with detailed info
  session (se)        Sessions with detailed info
  volume (vo, vol)    Volumes with detailed info
  workspace (ws)      Workspaces with detailed info

Examples:
  sgs des all                     # Describe all resources
  sgs des me                      # Show your user info
  sgs des no                      # Describe all nodes
  sgs des node ferrari            # Describe specific node
  sgs des se                      # Describe all sessions
  sgs des vo                      # Describe all volumes
  sgs des volume ferrari/my-vol   # Describe specific volume
  sgs des ws                      # Describe all workspaces
  sgs des workspace               # Describe current workspace`,
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
	case "nodes", "node", "no":
		if name == "" {
			describeNodes(ctx, k8sClient)
		} else {
			describeNode(ctx, k8sClient, name, true)
		}
	case "volumes", "volume", "vo", "vol":
		if name == "" {
			describeVolumes(ctx, k8sClient)
		} else {
			describeVolume(ctx, k8sClient, name, true)
		}
	case "sessions", "session", "se":
		if name == "" {
			describeSessions(ctx, k8sClient)
		} else {
			describeSession(ctx, k8sClient, name, true)
		}
	case "workspaces", "workspace", "ws":
		if name == "" {
			describeWorkspaces(ctx, k8sClient)
		} else {
			describeWorkspace(ctx, k8sClient, name, true)
		}
	case "me":
		getMe(true)
	default:
		exitWithError(fmt.Sprintf("unknown resource type: %s", resource), nil)
	}
}
