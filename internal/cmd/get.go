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
	corev1 "k8s.io/api/core/v1"
)

var getCmd = &cobra.Command{
	Use:   "get <resource> [name]",
	Short: "Display resources",
	Long: `Display one or more resources.

Resource types (aliases):
  all                 List all resources
  me                  Show your user information
  node (no)           Worker nodes in the cluster
  session (se)        Running sessions (edit/run pods)
  volume (vo, vol)    Volumes in current workspace
  workspace (ws)      Accessible workspaces
  current-workspace   Current workspace info

Examples:
  sgs get all                     # List all resources
  sgs get no                      # List all nodes
  sgs get node ferrari            # Get specific node info
  sgs get vo                      # List all volumes
  sgs get volume ferrari/my-vol   # Get specific volume info
  sgs get se                      # List all sessions
  sgs get ws                      # List all workspaces
  sgs get me                      # Show your user info`,
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

	// get always shows table format (even for single items)
	switch resource {
	case "all":
		getAll(ctx, k8sClient, false)
	case "nodes", "node", "no":
		getNodes(ctx, k8sClient, false, name) // name is filter (empty = all)
	case "volumes", "volume", "vo", "vol":
		getVolumes(ctx, k8sClient, false, name) // name is filter (empty = all)
	case "sessions", "session", "se":
		getSessions(ctx, k8sClient, false, name) // name is filter (empty = all)
	case "workspaces", "workspace", "ws":
		getWorkspaces(ctx, k8sClient, false, name) // name is filter (empty = all)
	case "current-workspace":
		describeWorkspace(ctx, k8sClient, "", false) // special case: detailed format
	case "me":
		getMe(false)
	default:
		exitWithError(fmt.Sprintf("unknown resource type: %s", resource), nil)
	}
}

func getNodes(ctx context.Context, k8sClient *client.Client, verbose bool, filterName string) {
	nodes, err := node.ListWorkerNodes(ctx, k8sClient)
	if err != nil {
		exitWithError("", err)
	}

	if len(nodes) == 0 {
		fmt.Println("No worker nodes found")
		return
	}

	// Filter nodes if a specific name is provided
	if filterName != "" {
		found := false
		for _, n := range nodes {
			if n.Name == filterName {
				nodes = []corev1.Node{n}
				found = true
				break
			}
		}
		if !found {
			exitWithError(fmt.Sprintf("node %q not found", filterName), nil)
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if verbose {
		fmt.Fprintln(w, "NAME\tACCESS\tSTATUS\tCPU (alloc/cap)\tMEM (alloc/cap)\tGPU (alloc/cap)\tGPU MEM (alloc/cap)")
	} else {
		fmt.Fprintln(w, "NAME\tACCESS\tSTATUS\tGPU (alloc/cap)\tGPU MEM (alloc/cap)")
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

		// Format metrics strings
		cpuStr := formatCPUMetrics(info)
		memStr := formatMemMetrics(info)
		gpuStr := formatGPUMetrics(info)
		gpuMemStr := formatGPUMemMetrics(info)

		if verbose {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				n.Name, access, status,
				cpuStr, memStr, gpuStr, gpuMemStr)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				n.Name, access, status,
				gpuStr, gpuMemStr)
		}
	}
	w.Flush()

	if verbose {
		fmt.Println("\nNote: 'alloc' shows sum of pod limits (may exceed capacity due to oversubscription).")
	}
}

// formatCPUMetrics formats CPU metrics as "alloc/cap"
func formatCPUMetrics(info *node.ResourceInfo) string {
	return fmt.Sprintf("%.1f/%.1f", info.CPUAlloc, info.CPUCapacity)
}

// formatMemMetrics formats memory metrics as "alloc/cap GiB"
func formatMemMetrics(info *node.ResourceInfo) string {
	return fmt.Sprintf("%.1f/%.1fGiB", info.MemAlloc, info.MemCapacity)
}

// formatGPUMetrics formats GPU metrics as "alloc/cap" or "-" if no GPU
func formatGPUMetrics(info *node.ResourceInfo) string {
	if info.GPUCapacity == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/%d", info.GPUAlloc, info.GPUCapacity)
}

// formatGPUMemMetrics formats GPU memory metrics as "alloc/cap GiB" or "-" if no GPU
func formatGPUMemMetrics(info *node.ResourceInfo) string {
	if info.GPUCapacity == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f/%.1fGiB", info.GPUMemAlloc, info.GPUMemCapacity)
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

func getVolumes(ctx context.Context, k8sClient *client.Client, verbose bool, filterPath string) {
	volumes, err := volume.List(ctx, k8sClient)
	if err != nil {
		exitWithError("", err)
	}

	if len(volumes) == 0 {
		fmt.Println("No volumes found in current workspace")
		return
	}

	// Filter volumes if a specific path is provided (node/volume format)
	if filterPath != "" {
		filterNode, filterName, err := volume.ParseVolumePath(filterPath)
		if err != nil {
			exitWithError("invalid volume path", err)
		}
		found := false
		for _, v := range volumes {
			if v.NodeName == filterNode && v.VolumeName == filterName {
				volumes = []volume.VolumeInfo{v}
				found = true
				break
			}
		}
		if !found {
			exitWithError(fmt.Sprintf("volume %q not found in current workspace", filterPath), nil)
		}
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
	describeNode(ctx, k8sClient, nodeName, verbose)
}

func describeNode(ctx context.Context, k8sClient *client.Client, nodeName string, verbose bool) {
	info, err := node.GetResourceInfo(ctx, k8sClient, nodeName)
	if err != nil {
		exitWithError("", err)
	}

	fmt.Printf("Node: %s\n", nodeName)
	fmt.Printf("  Access:  %s\n", formatNodeAccess(info.Group))
	fmt.Printf("  CPU:     %s\n", formatCPUMetrics(info))
	fmt.Printf("  Memory:  %s\n", formatMemMetrics(info))
	if info.GPUCapacity > 0 {
		fmt.Printf("  GPU:     %s\n", formatGPUMetrics(info))
		fmt.Printf("  GPU Mem: %s\n", formatGPUMemMetrics(info))
		if info.GPUType != "" {
			fmt.Printf("  GPU Type: %s\n", info.GPUType)
		}
	} else {
		fmt.Printf("  GPU:     (none)\n")
	}

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

func describeVolume(ctx context.Context, k8sClient *client.Client, volumePath string, verbose bool) {
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
		sessions, err := session.ListByVolume(ctx, k8sClient, volumeName)
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

func describeSession(ctx context.Context, k8sClient *client.Client, sessionName string, verbose bool) {
	// Convert node/volume path format to pod name format (node-volume)
	podName := sessionName
	if strings.Contains(sessionName, "/") {
		parts := strings.SplitN(sessionName, "/", 2)
		podName = parts[0] + "-" + parts[1]
	}

	s, err := session.Get(ctx, k8sClient, podName)
	if err != nil {
		exitWithError("", err)
	}

	// Display with original format (node/volume) for consistency
	displayName := fmt.Sprintf("%s/%s", s.Node, s.VolumeName)
	fmt.Printf("Session: %s\n", displayName)
	fmt.Printf("  Type:   %s\n", s.Type)
	fmt.Printf("  Volume: %s\n", s.VolumeName)
	fmt.Printf("  Node:   %s\n", s.Node)
	fmt.Printf("  Status: %s\n", s.Status)
	fmt.Printf("  GPUs:   %d\n", s.GPUs)
	fmt.Printf("  Age:    %s\n", s.Age)
}

func getWorkspaces(ctx context.Context, k8sClient *client.Client, verbose bool, filterName string) {
	workspaces, err := workspace.List(ctx, k8sClient)
	if err != nil {
		exitWithError("", err)
	}

	if len(workspaces) == 0 {
		fmt.Println("No accessible workspaces found")
		return
	}

	// Filter workspaces if a specific name is provided
	if filterName != "" {
		found := false
		for _, ws := range workspaces {
			if ws.Name == filterName {
				workspaces = []workspace.WorkspaceInfo{ws}
				found = true
				break
			}
		}
		if !found {
			exitWithError(fmt.Sprintf("workspace %q not found or access denied", filterName), nil)
		}
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

func describeWorkspace(ctx context.Context, k8sClient *client.Client, name string, verbose bool) {
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

func getSessions(ctx context.Context, k8sClient *client.Client, verbose bool, filterName string) {
	sessions, err := session.List(ctx, k8sClient)
	if err != nil {
		exitWithError("", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions found in current workspace")
		return
	}

	// Filter sessions if a specific name is provided (node/volume or pod name format)
	if filterName != "" {
		found := false
		for _, s := range sessions {
			// Match by pod name or by node/volume path
			sessionPath := fmt.Sprintf("%s/%s", s.Node, s.VolumeName)
			if s.PodName == filterName || sessionPath == filterName {
				sessions = []session.SessionInfo{s}
				found = true
				break
			}
		}
		if !found {
			exitWithError(fmt.Sprintf("session %q not found in current workspace", filterName), nil)
		}
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

// describeNodes shows detailed info for all nodes (concatenated describe output)
func describeNodes(ctx context.Context, k8sClient *client.Client) {
	nodes, err := node.ListWorkerNodes(ctx, k8sClient)
	if err != nil {
		exitWithError("", err)
	}

	if len(nodes) == 0 {
		fmt.Println("No worker nodes found")
		return
	}

	for i, n := range nodes {
		if i > 0 {
			fmt.Println() // Separator between nodes
		}
		describeNode(ctx, k8sClient, n.Name, true)
	}
}

// describeVolumes shows detailed info for all volumes (concatenated describe output)
func describeVolumes(ctx context.Context, k8sClient *client.Client) {
	volumes, err := volume.List(ctx, k8sClient)
	if err != nil {
		exitWithError("", err)
	}

	if len(volumes) == 0 {
		fmt.Println("No volumes found in current workspace")
		return
	}

	for i, v := range volumes {
		if i > 0 {
			fmt.Println() // Separator between volumes
		}
		volumePath := fmt.Sprintf("%s/%s", v.NodeName, v.VolumeName)
		describeVolume(ctx, k8sClient, volumePath, true)
	}
}

// describeSessions shows detailed info for all sessions (concatenated describe output)
func describeSessions(ctx context.Context, k8sClient *client.Client) {
	sessions, err := session.List(ctx, k8sClient)
	if err != nil {
		exitWithError("", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions found in current workspace")
		return
	}

	for i, s := range sessions {
		if i > 0 {
			fmt.Println() // Separator between sessions
		}
		// Use PodName directly since that's what session.Get expects
		describeSession(ctx, k8sClient, s.PodName, true)
	}
}

// describeWorkspaces shows detailed info for all workspaces (concatenated describe output)
func describeWorkspaces(ctx context.Context, k8sClient *client.Client) {
	workspaces, err := workspace.List(ctx, k8sClient)
	if err != nil {
		exitWithError("", err)
	}

	if len(workspaces) == 0 {
		fmt.Println("No accessible workspaces found")
		return
	}

	for i, ws := range workspaces {
		if i > 0 {
			fmt.Println() // Separator between workspaces
		}
		describeWorkspace(ctx, k8sClient, ws.Name, true)
	}
}

// getAll displays all resources (nodes, volumes, sessions, workspaces)
func getAll(ctx context.Context, k8sClient *client.Client, verbose bool) {
	// Get current workspace for header
	currentWS := k8sClient.Namespace

	// Workspaces
	fmt.Println("--- Workspaces ---")
	getWorkspaces(ctx, k8sClient, verbose, "")
	fmt.Println()

	// Nodes
	fmt.Println("--- Nodes ---")
	getNodes(ctx, k8sClient, verbose, "")
	fmt.Println()

	// Volumes
	fmt.Printf("--- Volumes (workspace: %s) ---\n", currentWS)
	getVolumes(ctx, k8sClient, verbose, "")
	fmt.Println()

	// Sessions
	fmt.Printf("--- Sessions (workspace: %s) ---\n", currentWS)
	getSessions(ctx, k8sClient, verbose, "")
}
