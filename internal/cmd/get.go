package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/bacchus-snu/sgs-cli/internal/node"
	"github.com/bacchus-snu/sgs-cli/internal/session"
	"github.com/bacchus-snu/sgs-cli/internal/user"
	"github.com/bacchus-snu/sgs-cli/internal/volume"
	"github.com/bacchus-snu/sgs-cli/internal/workspace"
	"github.com/spf13/cobra"
)

var getCmd = &cobra.Command{
	Use:   "get <resource> [name]",
	Short: "Display resources",
	Long: `Display one or more resources.

Resource types:
  all                - List all resources (nodes, volumes, sessions, workspaces)
  nodes              - List all worker nodes in the cluster
  node <name>        - Get details for a specific node
  volumes            - List all volumes in current workspace
  volume <node/vol>  - Get details for a specific volume
  sessions           - List all running sessions (edit/run pods)
  session <name>     - Get details for a specific session
  workspaces         - List all accessible workspaces
  current-workspace  - Get current workspace info
  me                 - Show your user information

Examples:
  # List all resources
  sgs get all

  # List all worker nodes
  sgs get nodes

  # Get specific node info
  sgs get node ferrari

  # List all volumes in current workspace
  sgs get volumes

  # Get specific volume info
  sgs get volume ferrari/my-volume

  # List all running sessions
  sgs get sessions

  # Get specific session info
  sgs get session edit-ferrari-my-volume

  # List all accessible workspaces
  sgs get workspaces

  # Get current workspace info
  sgs get current-workspace

  # Show your user info
  sgs get me`,
	Args: cobra.RangeArgs(1, 2),
	Run:  runGet,
}

func runGet(cmd *cobra.Command, args []string) {
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
		getAll(ctx, k8sClient, false)
	case "nodes", "node":
		if name == "" {
			getNodes(ctx, k8sClient, false)
		} else {
			getNode(ctx, k8sClient, name, false)
		}
	case "volumes", "volume":
		if name == "" {
			getVolumes(ctx, k8sClient, false)
		} else {
			getVolume(ctx, k8sClient, name, false)
		}
	case "sessions", "session":
		if name == "" {
			getSessions(ctx, k8sClient, false)
		} else {
			getSession(ctx, k8sClient, name, false)
		}
	case "workspaces", "workspace":
		if name == "" {
			getWorkspaces(ctx, k8sClient, false)
		} else {
			getWorkspace(ctx, k8sClient, name, false)
		}
	case "current-workspace":
		getWorkspace(ctx, k8sClient, "", false)
	case "me":
		getMe(false)
	default:
		exitWithError(fmt.Sprintf("unknown resource type: %s", resource), nil)
	}
}

func getNodes(ctx context.Context, k8sClient *client.Client, verbose bool) {
	nodes, err := node.ListWorkerNodes(ctx, k8sClient)
	if err != nil {
		exitWithError("", err)
	}

	if len(nodes) == 0 {
		fmt.Println("No worker nodes found")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if verbose {
		fmt.Fprintln(w, "NAME\tACCESS\tSTATUS\tCPU (used/total)\tMEMORY (used/total)\tGPU (used/total)")
	} else {
		fmt.Fprintln(w, "NAME\tACCESS\tSTATUS\tGPU (used/total)")
	}

	for _, n := range nodes {
		status := "Ready"
		for _, cond := range n.Status.Conditions {
			if cond.Type == "Ready" && cond.Status != "True" {
				status = "NotReady"
				break
			}
		}

		// Get node group from label and format access display
		group := n.Labels["node-restriction.kubernetes.io/nodegroup"]
		access := formatNodeAccess(group)

		info, err := node.GetResourceInfo(ctx, k8sClient, n.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to get resource info for %s: %v\n", n.Name, err)
			continue
		}

		if verbose {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s/%s\t%s/%s\t%d/%d\n",
				n.Name, access, status,
				info.CPUUsed, info.CPUTotal,
				info.MemoryUsed, info.MemoryTotal,
				info.GPUUsed, info.GPUTotal)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d/%d\n",
				n.Name, access, status,
				info.GPUUsed, info.GPUTotal)
		}
	}
	w.Flush()

	if verbose {
		fmt.Println("\nNote: CPU/memory usage shows total limits. Due to oversubscription, usage may exceed physical capacity.")
	}
}

// formatNodeAccess formats the node access for display based on the new access rules.
// - Nodes with "undergraduate" label: accessible by all workspaces (graduate + undergraduate)
// - Nodes with other/no label: accessible only by graduate workspaces
func formatNodeAccess(group string) string {
	if group == "undergraduate" {
		return "graduate/undergraduate"
	}
	return "graduate"
}

// formatWorkspaceAccess formats the workspace access for display.
// - Graduate workspaces (or no annotation) can access all nodes
// - Undergraduate workspaces can only access undergraduate nodes
func formatWorkspaceAccess(nodeGroup string) string {
	if nodeGroup == "graduate" || nodeGroup == "" {
		return "graduate/undergraduate"
	}
	if nodeGroup == "undergraduate" {
		return "undergraduate"
	}
	return nodeGroup
}

func getVolumes(ctx context.Context, k8sClient *client.Client, verbose bool) {
	volumes, err := volume.List(ctx, k8sClient)
	if err != nil {
		exitWithError("", err)
	}

	if len(volumes) == 0 {
		fmt.Println("No volumes found in current workspace")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if verbose {
		fmt.Fprintln(w, "NODE\tNAME\tTYPE\tSTATUS\tSIZE\tIMAGE\tAGE")
	} else {
		fmt.Fprintln(w, "NODE\tNAME\tTYPE\tSTATUS\tSIZE")
	}

	for _, v := range volumes {
		volType := "data"
		if v.IsOSVolume {
			volType = "os"
		}
		if verbose {
			image := v.Image
			if !v.IsOSVolume {
				image = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				v.NodeName, v.VolumeName, volType, v.Status, v.Size, image, v.Age)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				v.NodeName, v.VolumeName, volType, v.Status, v.Size)
		}
	}
	w.Flush()
}

func getNodeInfo(ctx context.Context, k8sClient *client.Client, nodeName string, verbose bool) {
	getNode(ctx, k8sClient, nodeName, verbose)
}

func getNode(ctx context.Context, k8sClient *client.Client, nodeName string, verbose bool) {
	info, err := node.GetResourceInfo(ctx, k8sClient, nodeName)
	if err != nil {
		exitWithError("", err)
	}

	fmt.Printf("Node: %s\n", nodeName)
	fmt.Printf("  Access:  %s\n", formatNodeAccess(info.Group))
	fmt.Printf("  CPU:     %s / %s\n", info.CPUUsed, info.CPUTotal)
	fmt.Printf("  Memory:  %s / %s\n", info.MemoryUsed, info.MemoryTotal)
	fmt.Printf("  GPU:     %d / %d\n", info.GPUUsed, info.GPUTotal)
	fmt.Printf("  Storage: %s / %s\n", info.StorageUsed, info.StorageTotal)

	if verbose {
		fmt.Printf("\nVolumes on this node:\n")
		volumes, err := volume.ListByNode(ctx, k8sClient, nodeName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to list volumes: %v\n", err)
			return
		}
		if len(volumes) == 0 {
			fmt.Println("  (none)")
		} else {
			for _, v := range volumes {
				volType := "data"
				if v.IsOSVolume {
					volType = "os"
				}
				fmt.Printf("  - %s [%s] (%s)\n", v.VolumeName, volType, v.Status)
			}
		}

		fmt.Printf("\nSessions on this node:\n")
		sessions, err := session.ListByNode(ctx, k8sClient, nodeName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to list sessions: %v\n", err)
			return
		}
		if len(sessions) == 0 {
			fmt.Println("  (none)")
		} else {
			for _, s := range sessions {
				fmt.Printf("  - %s [%s] (%s)\n", s.VolumeName, s.Type, s.Status)
			}
		}
	}
}

func getMe(verbose bool) {
	u, err := user.GetCurrentUser()
	if err != nil {
		exitWithError("failed to get user info", err)
	}

	fmt.Printf("User:   %s\n", u.Username)
	fmt.Printf("ID:     %s\n", u.Sub)
	fmt.Printf("Groups: %s\n", strings.Join(u.Groups, ", "))
}

func getVolume(ctx context.Context, k8sClient *client.Client, volumePath string, verbose bool) {
	// Parse node/volume path
	nodeName, volumeName, err := volume.ParseVolumePath(volumePath)
	if err != nil {
		exitWithError("invalid volume path", err)
	}

	v, err := volume.Get(ctx, k8sClient, nodeName, volumeName)
	if err != nil {
		exitWithError("", err)
	}

	fmt.Printf("Volume: %s/%s\n", nodeName, volumeName)
	fmt.Printf("  Type:   %s\n", map[bool]string{true: "os", false: "data"}[v.IsOSVolume])
	fmt.Printf("  Node:   %s\n", v.NodeName)
	fmt.Printf("  Status: %s\n", v.Status)
	fmt.Printf("  Size:   %s\n", v.Size)
	if v.IsOSVolume {
		fmt.Printf("  Image:  %s\n", v.Image)
	}
	fmt.Printf("  Age:    %s\n", v.Age)

	if verbose {
		fmt.Printf("\nSessions using this volume:\n")
		sessions, err := session.ListByVolume(ctx, k8sClient, volumePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to list sessions: %v\n", err)
			return
		}
		if len(sessions) == 0 {
			fmt.Println("  (none)")
		} else {
			for _, s := range sessions {
				fmt.Printf("  - %s [%s] (%s)\n", s.VolumeName, s.Type, s.Status)
			}
		}
	}
}

func getSession(ctx context.Context, k8sClient *client.Client, sessionName string, verbose bool) {
	s, err := session.Get(ctx, k8sClient, sessionName)
	if err != nil {
		exitWithError("", err)
	}

	fmt.Printf("Session: %s\n", sessionName)
	fmt.Printf("  Type:   %s\n", s.Type)
	fmt.Printf("  Volume: %s\n", s.VolumeName)
	fmt.Printf("  Node:   %s\n", s.Node)
	fmt.Printf("  Status: %s\n", s.Status)
	fmt.Printf("  GPUs:   %d\n", s.GPUs)
	fmt.Printf("  Age:    %s\n", s.Age)
}

func getWorkspaces(ctx context.Context, k8sClient *client.Client, verbose bool) {
	workspaces, err := workspace.List(ctx, k8sClient)
	if err != nil {
		exitWithError("", err)
	}

	if len(workspaces) == 0 {
		fmt.Println("No accessible workspaces found")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if verbose {
		fmt.Fprintln(w, "NAME\tACCESS\tGPU QUOTA\tCPU QUOTA\tMEM QUOTA")
		for _, ws := range workspaces {
			current := ""
			if ws.Name == k8sClient.Namespace {
				current = " (current)"
			}
			fmt.Fprintf(w, "%s%s\t%s\t%d\t%s\t%s\n",
				ws.Name, current, formatWorkspaceAccess(ws.NodeGroup), ws.GPUQuota, ws.CPUQuota, ws.MemQuota)
		}
	} else {
		fmt.Fprintln(w, "NAME\tACCESS\tGPU QUOTA")
		for _, ws := range workspaces {
			current := ""
			if ws.Name == k8sClient.Namespace {
				current = " (current)"
			}
			fmt.Fprintf(w, "%s%s\t%s\t%d\n", ws.Name, current, formatWorkspaceAccess(ws.NodeGroup), ws.GPUQuota)
		}
	}
	w.Flush()
}

func getWorkspace(ctx context.Context, k8sClient *client.Client, name string, verbose bool) {
	var ws *workspace.WorkspaceInfo
	var err error

	if name == "" {
		ws, err = workspace.GetCurrent(ctx, k8sClient)
		if err != nil {
			exitWithError("failed to get current workspace", err)
		}
	} else {
		ws, err = workspace.Get(ctx, k8sClient, name)
		if err != nil {
			exitWithError("workspace not found or access denied", err)
		}
	}

	current := ""
	if ws.Name == k8sClient.Namespace {
		current = " (current)"
	}

	fmt.Printf("Workspace: %s%s\n", ws.Name, current)
	fmt.Printf("  Access:    %s\n", formatWorkspaceAccess(ws.NodeGroup))
	fmt.Printf("  GPU Quota: %d\n", ws.GPUQuota)
	if verbose {
		fmt.Printf("  CPU Quota:  %s\n", ws.CPUQuota)
		fmt.Printf("  Mem Quota:  %s\n", ws.MemQuota)
	}
}

func getSessions(ctx context.Context, k8sClient *client.Client, verbose bool) {
	sessions, err := session.List(ctx, k8sClient)
	if err != nil {
		exitWithError("", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions found in current workspace")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if verbose {
		fmt.Fprintln(w, "NODE\tVOLUME\tMODE\tSTATUS\tGPU\tGPUMEM\tCOMMAND\tAGE")
	} else {
		fmt.Fprintln(w, "NODE\tVOLUME\tMODE\tSTATUS\tCOMMAND")
	}

	for _, s := range sessions {
		cmd := truncateCommand(s.Command, 40)
		if verbose {
			gpuMem := "-"
			if s.GPUMem > 0 {
				gpuMem = fmt.Sprintf("%dMi", s.GPUMem)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
				s.Node, s.VolumeName, s.Type, s.Status, s.GPUs, gpuMem, cmd, s.Age)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				s.Node, s.VolumeName, s.Type, s.Status, cmd)
		}
	}
	w.Flush()
}

// truncateCommand truncates a command string for display
func truncateCommand(cmd string, maxLen int) string {
	if len(cmd) <= maxLen {
		return cmd
	}
	return cmd[:maxLen-3] + "..."
}

// getAll displays all resources (nodes, volumes, sessions, workspaces)
func getAll(ctx context.Context, k8sClient *client.Client, verbose bool) {
	// Get current workspace for header
	currentWS := k8sClient.Namespace

	// Workspaces
	fmt.Println("--- Workspaces ---")
	getWorkspaces(ctx, k8sClient, verbose)
	fmt.Println()

	// Nodes
	fmt.Println("--- Nodes ---")
	getNodes(ctx, k8sClient, verbose)
	fmt.Println()

	// Volumes
	fmt.Printf("--- Volumes (workspace: %s) ---\n", currentWS)
	getVolumes(ctx, k8sClient, verbose)
	fmt.Println()

	// Sessions
	fmt.Printf("--- Sessions (workspace: %s) ---\n", currentWS)
	getSessions(ctx, k8sClient, verbose)
}
