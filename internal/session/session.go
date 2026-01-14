// Package session provides session listing and info for SGS.
package session

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/bacchus-snu/sgs-cli/internal/sgs"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SessionType represents the type of session
type SessionType string

// Session type values - these use the hardcoded defaults and are
// compared against the runtime sgs.SessionModeEdit/Run values.
var (
	SessionTypeEdit SessionType = "edit"
	SessionTypeRun  SessionType = "run"
)

// SessionInfo represents information about an SGS session (running pod)
type SessionInfo struct {
	PodName    string // Internal pod name
	VolumeName string // Volume name without node prefix
	Type       SessionType
	Node       string
	Status     string
	GPUs       int
	GPUMem     int64 // GPU memory in MiB (HAMi)
	Age        string
	Command    string // Command being run (for run sessions)
}

// LogsOptions holds options for getting logs
type LogsOptions struct {
	Follow bool
	Tail   int64
}

// List returns all sessions (pods) in the current namespace
// Excludes system pods (init, copy) by filtering on session-mode label
func List(ctx context.Context, c *client.Client) ([]SessionInfo, error) {
	pods, err := client.RetryWithContext(ctx, func() (*corev1.PodList, error) {
		return c.Clientset.CoreV1().Pods(c.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=sgs,%s", sgs.LabelManagedBy, sgs.LabelSessionMode),
		})
	})
	if err != nil {
		return nil, client.FormatK8sError(err, "list", "sessions", c.Namespace)
	}

	var sessions []SessionInfo
	for _, pod := range pods.Items {
		sessions = append(sessions, podToSessionInfo(&pod))
	}

	return sessions, nil
}

// ListByVolume returns all sessions for a specific volume
func ListByVolume(ctx context.Context, c *client.Client, volumeName string) ([]SessionInfo, error) {
	pods, err := client.RetryWithContext(ctx, func() (*corev1.PodList, error) {
		return c.Clientset.CoreV1().Pods(c.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=sgs,%s=%s,%s", sgs.LabelManagedBy, sgs.LabelVolumeName, volumeName, sgs.LabelSessionMode),
		})
	})
	if err != nil {
		return nil, client.FormatK8sError(err, "list", "sessions", c.Namespace)
	}

	var sessions []SessionInfo
	for _, pod := range pods.Items {
		sessions = append(sessions, podToSessionInfo(&pod))
	}

	return sessions, nil
}

// ListByNode returns all sessions on a specific node
func ListByNode(ctx context.Context, c *client.Client, nodeName string) ([]SessionInfo, error) {
	pods, err := client.RetryWithContext(ctx, func() (*corev1.PodList, error) {
		return c.Clientset.CoreV1().Pods(c.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=sgs,%s", sgs.LabelManagedBy, sgs.LabelSessionMode),
			FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
		})
	})
	if err != nil {
		return nil, client.FormatK8sError(err, "list", "sessions", c.Namespace)
	}

	var sessions []SessionInfo
	for _, pod := range pods.Items {
		sessions = append(sessions, podToSessionInfo(&pod))
	}

	return sessions, nil
}

// Get returns information about a specific session
func Get(ctx context.Context, c *client.Client, sessionName string) (*SessionInfo, error) {
	pod, err := client.RetryWithContext(ctx, func() (*corev1.Pod, error) {
		return c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, sessionName, metav1.GetOptions{})
	})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("session %q not found in workspace %q", sessionName, c.Namespace)
		}
		return nil, client.FormatK8sError(err, "get", "session", c.Namespace)
	}

	// Check if it's an SGS-managed pod
	if pod.Labels[sgs.LabelManagedBy] != "sgs" {
		return nil, fmt.Errorf("session not found: %s", sessionName)
	}

	info := podToSessionInfo(pod)
	return &info, nil
}

// Logs returns logs from a session
func Logs(ctx context.Context, c *client.Client, sessionName string, opts LogsOptions) (string, error) {
	// Verify it's an SGS session
	_, err := Get(ctx, c, sessionName)
	if err != nil {
		return "", err
	}

	logOpts := &corev1.PodLogOptions{
		Container: "main",
	}
	if opts.Follow {
		logOpts.Follow = true
	}
	if opts.Tail >= 0 {
		logOpts.TailLines = &opts.Tail
	}

	req := c.Clientset.CoreV1().Pods(c.Namespace).GetLogs(sessionName, logOpts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", client.FormatK8sError(err, "get", "logs", c.Namespace)
	}
	defer stream.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(stream); err != nil {
		return "", fmt.Errorf("failed to read logs: %w", err)
	}

	return buf.String(), nil
}

// podToSessionInfo converts a pod to SessionInfo
func podToSessionInfo(pod *corev1.Pod) SessionInfo {
	info := SessionInfo{
		PodName: pod.Name,
		Node:    pod.Spec.NodeName,
		Status:  string(pod.Status.Phase),
		Age:     formatAge(time.Since(pod.CreationTimestamp.Time)),
	}

	// Get volume name from label (this is the PVC name: <node>-<volume>)
	// Extract just the volume part by removing node prefix
	pvcName := pod.Labels[sgs.LabelVolumeName]
	nodeName := pod.Labels[sgs.LabelNodeName]
	if nodeName != "" && strings.HasPrefix(pvcName, nodeName+"-") {
		info.VolumeName = strings.TrimPrefix(pvcName, nodeName+"-")
	} else {
		info.VolumeName = pvcName
	}

	// Use node from label if spec.nodeName is empty (pending pods)
	if info.Node == "" {
		info.Node = nodeName
	}

	// Get session mode from label
	mode := pod.Labels[sgs.LabelSessionMode]
	if mode == "run" {
		info.Type = SessionTypeRun
	} else {
		info.Type = SessionTypeEdit
	}

	// Extract command and GPU count from containers
	for _, container := range pod.Spec.Containers {
		if container.Name == "work-node" {
			// For run sessions, the user command is embedded in args
			// The command is inside the proot bash -c block at the end
			if len(container.Args) > 0 {
				arg := container.Args[len(container.Args)-1]
				// Look for the pattern in proot's bash -c command
				// The command is after "ldconfig 2>/dev/null || true\n"
				if idx := strings.Index(arg, "ldconfig 2>/dev/null || true"); idx != -1 {
					remaining := arg[idx:]
					// Find the newline after ldconfig line
					if nlIdx := strings.Index(remaining, "\n"); nlIdx != -1 {
						cmd := strings.TrimSpace(remaining[nlIdx+1:])
						// Remove trailing whitespace and closing quote/brace
						cmd = strings.TrimRight(cmd, " \n\t\"")
						// Remove surrounding quotes if present
						if len(cmd) >= 2 && cmd[0] == '\'' && cmd[len(cmd)-1] == '\'' {
							cmd = cmd[1 : len(cmd)-1]
						}
						info.Command = cmd
					}
				}
			}
		}
		// Count GPUs
		if gpuQty, ok := container.Resources.Limits["nvidia.com/gpu"]; ok {
			info.GPUs = int(gpuQty.Value())
		}
		// Get GPU memory
		if gpuMemQty, ok := container.Resources.Limits["nvidia.com/gpumem"]; ok {
			info.GPUMem = gpuMemQty.Value()
		}
	}

	return info
}

// formatAge formats a duration into a human-readable age string
func formatAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
