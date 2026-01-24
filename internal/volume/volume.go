// Package volume provides volume and session management for SGS.
package volume

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bacchus-snu/sgs-cli/internal/cleanup"
	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/bacchus-snu/sgs-cli/internal/sgs"
	"github.com/bacchus-snu/sgs-cli/internal/workspace"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// Re-export values for backward compatibility.
// These are initialized to the default values and updated when config is loaded.
var (
	SessionModeEdit = "edit"
	SessionModeRun  = "run"
	DefaultImage    = "nvcr.io/nvidia/cuda:12.5.0-base-ubuntu22.04"
)

// isForbiddenError checks if an error is a Forbidden/permission error
func isForbiddenError(err error) bool {
	if statusErr, ok := err.(*errors.StatusError); ok {
		return statusErr.ErrStatus.Reason == "Forbidden"
	}
	errStr := err.Error()
	return strings.Contains(errStr, "forbidden") || strings.Contains(errStr, "Forbidden")
}

// workspaceExists checks if a workspace exists (but doesn't check permission)
func workspaceExists(ctx context.Context, c *client.Client, name string) bool {
	return workspace.Exists(ctx, c, name)
}

// VolumeInfo represents information about an SGS volume
type VolumeInfo struct {
	NodeName   string
	VolumeName string
	Status     string
	Size       string
	Image      string // OS image from annotation (empty for normal volumes)
	Age        string
	IsOSVolume bool
}

// CreateOptions holds options for creating a volume
type CreateOptions struct {
	NodeName   string
	VolumeName string
	Size       string
	Image      string // If empty, creates a normal volume (no pod); if set, creates OS volume
}

// EditOptions holds options for editing a volume
type EditOptions struct {
	NodeName   string
	VolumeName string
	Mounts     []MountOption // Additional volumes to mount
}

// RunOptions holds options for running a volume with GPU
type RunOptions struct {
	NodeName   string
	VolumeName string
	GPUs       int           // Number of GPUs
	GPUMem     int64         // GPU memory in MiB (HAMi)
	Command    []string      // Command to run (optional, interactive if empty)
	Mounts     []MountOption // Additional volumes to mount
	PinCPU     int64         // Pinned CPU cores (0 = no pinning)
	PinMem     int64         // Pinned memory in bytes (0 = no pinning)
}

// MountOption represents a volume mount
type MountOption struct {
	SourceVolume string // Source PVC name
	MountPath    string // Path inside container
}

// LogsOptions holds options for getting logs
type LogsOptions struct {
	Follow bool
	Tail   int64
}

// pvcName returns the PVC name for a volume
// Format: <node>-<volume> to allow same volume name on different nodes
func pvcName(nodeName, volumeName string) string {
	return nodeName + "-" + volumeName
}

// ParseVolumePath parses a user input "node/volume" into node and volume names
func ParseVolumePath(path string) (nodeName, volumeName string, err error) {
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid volume path format, expected: <node>/<volume>")
	}
	return parts[0], parts[1], nil
}

// FormatVolumePath formats node and volume into "node/volume" display format
func FormatVolumePath(nodeName, volumeName string) string {
	return nodeName + "/" + volumeName
}

// List returns all volumes (PVCs) in the current namespace
func List(ctx context.Context, c *client.Client) ([]VolumeInfo, error) {
	// List ALL PVCs in namespace with retry
	pvcs, err := client.RetryWithContext(ctx, func() (*corev1.PersistentVolumeClaimList, error) {
		return c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).List(ctx, metav1.ListOptions{})
	})
	if err != nil {
		// For Forbidden errors, check if the workspace exists to provide better error message
		if isForbiddenError(err) && !client.IsNamespaceExplicitlySet() && c.Namespace == "default" {
			return nil, client.FormatK8sError(err, "list", "volumes", c.Namespace)
		}
		if isForbiddenError(err) {
			// Check if workspace exists (don't use workspace.Exists to avoid circular dependency)
			if exists := workspaceExists(ctx, c, c.Namespace); !exists {
				return nil, fmt.Errorf("workspace %q does not exist", c.Namespace)
			}
		}
		return nil, client.FormatK8sError(err, "list", "volumes", c.Namespace)
	}

	var volumes []VolumeInfo
	for _, pvc := range pvcs.Items {
		// Get node from selected-node annotation, fallback to label
		nodeName := pvc.Annotations[sgs.AnnotationSelectedNode]
		if nodeName == "" {
			nodeName = pvc.Labels[sgs.LabelNodeName]
		}

		// Get volume name from label (PVC name is <node>-<volume>)
		volumeName := pvc.Labels[sgs.LabelVolumeName]
		if volumeName == "" {
			// Fallback: try to extract from PVC name (for backwards compatibility)
			volumeName = pvc.Name
		}

		// Check if this is an OS volume (has image annotation)
		osImage := pvc.Annotations[sgs.AnnotationOSImage]
		isOSVolume := osImage != ""

		// Check if there's an init pod (volume is being initialized)
		initPodName := "init-" + pvc.Name
		initPod, initErr := c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, initPodName, metav1.GetOptions{})

		// Check if there's an associated pod
		pod, err := c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, pvc.Name, metav1.GetOptions{})
		status := string(pvc.Status.Phase) // Default to PVC status (Bound, Pending, etc.)

		if initErr == nil {
			// Init pod exists - show initializing status
			switch initPod.Status.Phase {
			case corev1.PodPending:
				status = "Initializing"
			case corev1.PodRunning:
				status = "Initializing"
			case corev1.PodFailed:
				status = "InitFailed"
			}
		} else if err == nil {
			// Regular pod exists, use pod status
			status = string(pod.Status.Phase)
		}

		size := "N/A"
		if storage, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
			size = storage.String()
		}

		age := formatAge(time.Since(pvc.CreationTimestamp.Time))

		volumes = append(volumes, VolumeInfo{
			NodeName:   nodeName,
			VolumeName: volumeName,
			Status:     status,
			Size:       size,
			Image:      osImage,
			Age:        age,
			IsOSVolume: isOSVolume,
		})
	}

	return volumes, nil
}

// formatAge formats a duration into a human-readable age string
func formatAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// ListByNode returns all volumes on a specific node
func ListByNode(ctx context.Context, c *client.Client, nodeName string) ([]VolumeInfo, error) {
	volumes, err := List(ctx, c)
	if err != nil {
		return nil, err
	}

	var filtered []VolumeInfo
	for _, v := range volumes {
		if v.NodeName == nodeName {
			filtered = append(filtered, v)
		}
	}

	return filtered, nil
}

// Get returns information about a specific volume by node and name
func Get(ctx context.Context, c *client.Client, nodeName, volumeName string) (*VolumeInfo, error) {
	pvc, err := client.RetryWithContext(ctx, func() (*corev1.PersistentVolumeClaim, error) {
		return c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Get(ctx, pvcName(nodeName, volumeName), metav1.GetOptions{})
	})
	if err != nil {
		return nil, fmt.Errorf("volume not found: %w", err)
	}

	// Check if this is an OS volume (has image annotation)
	osImage := pvc.Annotations[sgs.AnnotationOSImage]
	isOSVolume := osImage != ""

	// Check if there's an associated pod
	pod, err := client.RetryWithContext(ctx, func() (*corev1.Pod, error) {
		return c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, pvcName(nodeName, volumeName), metav1.GetOptions{})
	})
	status := string(pvc.Status.Phase)
	if err == nil {
		status = string(pod.Status.Phase)
	}

	size := "N/A"
	if storage, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		size = storage.String()
	}

	age := formatAge(time.Since(pvc.CreationTimestamp.Time))

	return &VolumeInfo{
		NodeName:   nodeName,
		VolumeName: volumeName,
		Status:     status,
		Size:       size,
		Image:      osImage,
		Age:        age,
		IsOSVolume: isOSVolume,
	}, nil
}

// Create creates a new volume (PVC only)
// If Image is set, stores the image in an annotation for the runtime wrapper to use,
// and creates a binder pod to trigger PVC binding.
func Create(ctx context.Context, c *client.Client, opts CreateOptions) error {
	// Validate node access before creating anything
	if err := validateNodeAccess(ctx, c, opts.NodeName); err != nil {
		return err
	}

	name := pvcName(opts.NodeName, opts.VolumeName)

	// Set defaults
	if opts.Size == "" {
		opts.Size = sgs.DefaultStorageSize
	}

	// Create labels
	labels := map[string]string{
		sgs.LabelManagedBy:  "sgs",
		sgs.LabelNodeName:   opts.NodeName,
		sgs.LabelVolumeName: opts.VolumeName,
	}

	// Create annotations
	annotations := make(map[string]string)
	if opts.Image != "" {
		// OS volume - store image in annotation for runtime wrapper
		annotations[sgs.AnnotationOSImage] = opts.Image
	}

	// Create PVC
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   c.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(opts.Size),
				},
			},
		},
	}

	_, err := c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		return client.FormatK8sError(err, "create", "volume", c.Namespace)
	}

	// For OS volumes, create a binder pod to trigger PVC binding and cache image
	if opts.Image != "" {
		// Register cleanup for PVC in case of interrupt during binding
		cleanup.Register(func(cleanupCtx context.Context) {
			fmt.Fprint(os.Stderr, "Cleaning up volume...")
			if err := c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Delete(cleanupCtx, name, metav1.DeleteOptions{}); err != nil {
				fmt.Fprintf(os.Stderr, " failed: %v\n", err)
			} else {
				fmt.Fprintln(os.Stderr, " done")
			}
		})

		binderPod := createBinderPodSpec(name, opts.NodeName, opts.Image, c.Namespace)
		podName := binderPod.Name
		_, err = c.Clientset.CoreV1().Pods(c.Namespace).Create(ctx, binderPod, metav1.CreateOptions{})
		if err != nil {
			// Cleanup PVC if pod creation fails
			cleanup.Unregister()
			_ = c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Delete(ctx, name, metav1.DeleteOptions{})
			return client.FormatK8sError(err, "create", "volume binding", c.Namespace)
		}

		// Register cleanup for binder pod
		cleanup.Register(func(cleanupCtx context.Context) {
			fmt.Fprint(os.Stderr, "Cleaning up binder pod...")
			if err := c.Clientset.CoreV1().Pods(c.Namespace).Delete(cleanupCtx, podName, metav1.DeleteOptions{}); err != nil {
				fmt.Fprintf(os.Stderr, " failed: %v\n", err)
			} else {
				fmt.Fprintln(os.Stderr, " done")
			}
		})

		// Wait for binder pod to complete (PVC will be bound)
		if err := waitForBinderPod(ctx, c, podName, 5*time.Minute); err != nil {
			// If interrupted, signal handler does cleanup - just wait and return
			if cleanup.WasInterrupted() {
				cleanup.WaitForCleanup()
				return nil
			}
			// Cleanup on failure
			cleanup.Unregister() // binder pod
			cleanup.Unregister() // PVC
			_ = c.Clientset.CoreV1().Pods(c.Namespace).Delete(ctx, podName, metav1.DeleteOptions{})
			_ = c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Delete(ctx, name, metav1.DeleteOptions{})
			return fmt.Errorf("volume binding failed: %w", err)
		}

		// Success - unregister cleanups and delete binder pod
		cleanup.Unregister() // binder pod
		cleanup.Unregister() // PVC
		_ = c.Clientset.CoreV1().Pods(c.Namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	}

	return nil
}

// waitForBinderPod waits for the binder pod to complete successfully or fail
func waitForBinderPod(ctx context.Context, c *client.Client, podName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		pod, err := c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get binder pod: %w", err)
		}

		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			return nil
		case corev1.PodFailed:
			// Get reason from container status
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
					return fmt.Errorf("binder pod failed: %s", cs.State.Terminated.Reason)
				}
				if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
					return fmt.Errorf("binder pod failed: %s - %s", cs.State.Waiting.Reason, cs.State.Waiting.Message)
				}
			}
			return fmt.Errorf("binder pod failed")
		case corev1.PodPending:
			// Check for image pull errors
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil {
					if cs.State.Waiting.Reason == "ImagePullBackOff" || cs.State.Waiting.Reason == "ErrImagePull" {
						return fmt.Errorf("failed to pull image: %s", cs.State.Waiting.Message)
					}
				}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			// continue polling
		}
	}

	return fmt.Errorf("timeout waiting for volume binding")
}

// createBinderPodSpec creates a pod that binds the PVC and caches the image (no os-volume annotation)
func createBinderPodSpec(pvcName, nodeName, image, namespace string) *corev1.Pod {
	podName := "bind-" + pvcName

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				sgs.LabelManagedBy:    "sgs",
				"sgs.snucse.org/mode": "bind",
			},
			// No os-volume annotation - runtime wrapper will not interfere
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": nodeName,
			},
			Containers: []corev1.Container{
				{
					Name:  "bind",
					Image: image,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("0"),
							corev1.ResourceMemory: resource.MustParse("0"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(sgs.EditCPULimit),
							corev1.ResourceMemory: resource.MustParse(sgs.EditMemoryLimit),
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "data",
							MountPath: "/mnt/data",
						},
					},
					Command: []string{"true"},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
}

// GetPVCInfo retrieves PVC info including OS image
func GetPVCInfo(ctx context.Context, c *client.Client, nodeName, volumeName string) (osImage string, err error) {
	pvc, err := c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Get(ctx, pvcName(nodeName, volumeName), metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("volume not found: %w", err)
	}
	return pvc.Annotations[sgs.AnnotationOSImage], nil
}

// GetNodeResources returns total CPU cores, memory bytes, and GPU count for a node
func GetNodeResources(ctx context.Context, c *client.Client, nodeName string) (cpuCores int64, memoryBytes int64, gpuCount int64, err error) {
	node, err := c.Clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return 0, 0, 0, client.FormatK8sError(err, "get", "node", "cluster")
	}

	allocatable := node.Status.Allocatable
	cpuCores = allocatable.Cpu().Value()
	memoryBytes = allocatable.Memory().Value()
	if gpu, ok := allocatable[corev1.ResourceName("nvidia.com/gpu")]; ok {
		gpuCount = gpu.Value()
	}

	return cpuCores, memoryBytes, gpuCount, nil
}

// sessionPodName returns the pod name for a session
// Format: <node>-<volume> (one session per volume)
func sessionPodName(nodeName, volumeName string) string {
	return fmt.Sprintf("%s-%s", nodeName, volumeName)
}

// GetSessionMode returns the mode of an existing session, or empty string if no session exists
func GetSessionMode(ctx context.Context, c *client.Client, nodeName, volumeName string) (string, error) {
	podName := sessionPodName(nodeName, volumeName)
	pod, err := client.RetryWithContext(ctx, func() (*corev1.Pod, error) {
		return c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, podName, metav1.GetOptions{})
	})
	if err != nil {
		if errors.IsNotFound(err) {
			return "", nil // No session exists
		}
		return "", client.FormatK8sError(err, "check", "session", c.Namespace)
	}

	// Check if pod is still active
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return "", nil // Session is terminated
	}

	// Get mode from label, with fallback for backwards compatibility
	mode := pod.Labels[sgs.LabelSessionMode]
	if mode == "" {
		// Old pods without the mode label - determine mode from other indicators
		// If it has GPU resources, it's a run session; otherwise edit
		for _, container := range pod.Spec.Containers {
			if gpuLimit, ok := container.Resources.Limits["nvidia.com/gpu"]; ok && !gpuLimit.IsZero() {
				return SessionModeRun, nil
			}
		}
		return SessionModeEdit, nil
	}

	return mode, nil
}

// EditResult contains the result of starting an edit session
type EditResult struct {
	PodName  string
	Existing bool // true if attached to existing session
}

// RunResult contains the result of starting a run session
type RunResult struct {
	PodName string
}

// Edit starts an edit session for an OS volume (no GPU, limited CPU/memory)
// Returns the pod name and whether it's an existing session
func Edit(ctx context.Context, c *client.Client, opts EditOptions) (*EditResult, error) {
	// Validate node access
	if err := validateNodeAccess(ctx, c, opts.NodeName); err != nil {
		return nil, err
	}

	podName := sessionPodName(opts.NodeName, opts.VolumeName)
	pvc := pvcName(opts.NodeName, opts.VolumeName)

	// Get PVC info
	osImage, err := GetPVCInfo(ctx, c, opts.NodeName, opts.VolumeName)
	if err != nil {
		return nil, err
	}

	// Validate it's an OS volume
	if osImage == "" {
		return nil, fmt.Errorf("cannot edit: '%s/%s' is not an OS volume (no image configured)", opts.NodeName, opts.VolumeName)
	}

	// Check if pod already exists
	existingPod, err := c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, podName, metav1.GetOptions{})
	if err == nil {
		// Pod exists - check if it's still usable
		if existingPod.Status.Phase == corev1.PodRunning || existingPod.Status.Phase == corev1.PodPending {
			return &EditResult{PodName: podName, Existing: true}, nil
		}
		// Pod exists but is terminated - delete it first
		if err := c.Clientset.CoreV1().Pods(c.Namespace).Delete(ctx, podName, metav1.DeleteOptions{}); err != nil {
			return nil, fmt.Errorf("failed to cleanup terminated pod: %w", err)
		}
		// Wait a moment for deletion
		time.Sleep(time.Second)
	} else if !errors.IsNotFound(err) {
		return nil, client.FormatK8sError(err, "check", "session", c.Namespace)
	}

	// Create pod with edit mode resources
	pod := createEditPodSpec(podName, pvc, opts.NodeName, opts.VolumeName, osImage, opts.Mounts, c.Namespace)

	_, err = c.Clientset.CoreV1().Pods(c.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, client.FormatK8sError(err, "create", "session", c.Namespace)
	}

	return &EditResult{PodName: podName, Existing: false}, nil
}

// Run starts a GPU session for an OS volume
// Returns the run result with pod name
func Run(ctx context.Context, c *client.Client, opts RunOptions) (*RunResult, error) {
	// Validate node access
	if err := validateNodeAccess(ctx, c, opts.NodeName); err != nil {
		return nil, err
	}

	podName := sessionPodName(opts.NodeName, opts.VolumeName)
	pvc := pvcName(opts.NodeName, opts.VolumeName)

	// Get PVC info
	osImage, err := GetPVCInfo(ctx, c, opts.NodeName, opts.VolumeName)
	if err != nil {
		return nil, err
	}

	// Validate it's an OS volume
	if osImage == "" {
		return nil, fmt.Errorf("cannot run: '%s/%s' is not an OS volume (no image configured)", opts.NodeName, opts.VolumeName)
	}

	// Check if pod already exists
	existingPod, err := c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, podName, metav1.GetOptions{})
	if err == nil {
		// Pod exists - check if it's still usable
		if existingPod.Status.Phase == corev1.PodRunning || existingPod.Status.Phase == corev1.PodPending {
			return &RunResult{PodName: podName}, nil
		}
		// Pod exists but is terminated - delete it first
		if err := c.Clientset.CoreV1().Pods(c.Namespace).Delete(ctx, podName, metav1.DeleteOptions{}); err != nil {
			return nil, client.FormatK8sError(err, "cleanup", "session", c.Namespace)
		}
		// Wait a moment for deletion
		time.Sleep(time.Second)
	} else if !errors.IsNotFound(err) {
		return nil, client.FormatK8sError(err, "check", "session", c.Namespace)
	}

	// Get node resources to calculate CPU/memory limits
	totalCPU, totalMemory, totalGPU, err := GetNodeResources(ctx, c, opts.NodeName)
	if err != nil {
		return nil, err
	}

	if totalGPU == 0 {
		return nil, fmt.Errorf("node %s has no GPUs available", opts.NodeName)
	}

	// Calculate resources per GPU: (7/8) of resources divided by total GPUs
	// CPU limit = (7 * totalCPU * gpusRequested) / (8 * totalGPU)
	// Memory limit = (7 * totalMemory * gpusRequested) / (8 * totalGPU)
	cpuLimit := (7 * totalCPU * int64(opts.GPUs)) / (8 * totalGPU)
	memLimit := (7 * totalMemory * int64(opts.GPUs)) / (8 * totalGPU)

	// Apply pinning if requested (request = limit when pinned)
	cpuRequest := int64(0)
	memRequest := int64(0)
	if opts.PinCPU > 0 {
		cpuLimit = opts.PinCPU
		cpuRequest = opts.PinCPU
	}
	if opts.PinMem > 0 {
		memLimit = opts.PinMem
		memRequest = opts.PinMem
	}

	// Create pod with GPU resources
	pod := createRunPodSpec(podName, pvc, opts.NodeName, opts.VolumeName, osImage, opts.GPUs, opts.GPUMem, cpuLimit, memLimit, cpuRequest, memRequest, opts.Command, opts.Mounts, c.Namespace)

	_, err = c.Clientset.CoreV1().Pods(c.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, client.FormatK8sError(err, "create", "session", c.Namespace)
	}

	return &RunResult{PodName: podName}, nil
}

// createEditPodSpec creates a pod for edit mode (no GPU, limited resources)
// Uses SGS RuntimeClass which swaps the container rootfs to the PVC via sgs-runc-wrapper
func createEditPodSpec(podName, pvcName, nodeName, volumeName, image string, mounts []MountOption, namespace string) *corev1.Pod {
	// Build volume mounts and volumes
	// The boot-volume mount is a "beacon" - sgs-runc-wrapper uses its source as the new Root.Path
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "boot-volume",
			MountPath: "/var/lib/sgs/boot",
		},
	}
	volumes := []corev1.Volume{
		{
			Name: "boot-volume",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		},
	}

	// Add additional mounts
	for i, m := range mounts {
		volName := fmt.Sprintf("mount-%d", i)
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      volName,
			MountPath: m.MountPath,
		})
		volumes = append(volumes, corev1.Volume{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: m.SourceVolume,
				},
			},
		})
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				sgs.LabelManagedBy:   "sgs",
				sgs.LabelVolumeName:  pvcName,
				sgs.LabelNodeName:    nodeName,
				sgs.LabelSessionMode: SessionModeEdit,
			},
			Annotations: map[string]string{
				// Triggers sgs-runtime-wrapper to swap Root.Path to this PVC
				sgs.AnnotationOSVolume: pvcName,
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": nodeName,
			},
			Containers: []corev1.Container{
				{
					Name:  "main",
					Image: image,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("0"),
							corev1.ResourceMemory: resource.MustParse("0"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:                       resource.MustParse(sgs.EditCPULimit),
							corev1.ResourceMemory:                    resource.MustParse(sgs.EditMemoryLimit),
							corev1.ResourceName("nvidia.com/gpu"):    resource.MustParse("1"),
							corev1.ResourceName("nvidia.com/gpumem"): resource.MustParse("0"),
						},
					},
					VolumeMounts: volumeMounts,
					Command:      []string{"/bin/sh"},
					Stdin:        true,
					TTY:          true,
				},
			},
			Volumes:       volumes,
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
}

// createRunPodSpec creates a pod for run mode (with GPU)
// Uses SGS RuntimeClass which swaps the container rootfs to the PVC via sgs-runc-wrapper
func createRunPodSpec(podName, pvcName, nodeName, volumeName, image string, gpus int, gpuMem int64, cpuLimit, memLimit, cpuRequest, memRequest int64, command []string, mounts []MountOption, namespace string) *corev1.Pod {
	// Build volume mounts and volumes
	// The boot-volume mount is a "beacon" - sgs-runc-wrapper uses its source as the new Root.Path
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "boot-volume",
			MountPath: "/var/lib/sgs/boot",
		},
	}
	volumes := []corev1.Volume{
		{
			Name: "boot-volume",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		},
	}

	// Add additional mounts
	for i, m := range mounts {
		volName := fmt.Sprintf("mount-%d", i)
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      volName,
			MountPath: m.MountPath,
		})
		volumes = append(volumes, corev1.Volume{
			Name: volName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: m.SourceVolume,
				},
			},
		})
	}

	// Determine if this is interactive mode (no command) or batch mode (with command)
	interactive := len(command) == 0

	container := corev1.Container{
		Name:  "main",
		Image: image,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewQuantity(cpuRequest, resource.DecimalSI),
				corev1.ResourceMemory: *resource.NewQuantity(memRequest, resource.BinarySI),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:                       *resource.NewQuantity(cpuLimit, resource.DecimalSI),
				corev1.ResourceMemory:                    *resource.NewQuantity(memLimit, resource.BinarySI),
				corev1.ResourceName("nvidia.com/gpu"):    resource.MustParse(fmt.Sprintf("%d", gpus)),
				corev1.ResourceName("nvidia.com/gpumem"): *resource.NewQuantity(gpuMem, resource.DecimalSI),
			},
		},
		VolumeMounts: volumeMounts,
	}

	if interactive {
		// Interactive mode - shell
		container.Command = []string{"/bin/sh"}
		container.Stdin = true
		container.TTY = true
	} else {
		// Batch mode - execute user command
		container.Command = []string{"/bin/sh", "-c"}
		container.Args = []string{strings.Join(command, " ")}
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				sgs.LabelManagedBy:   "sgs",
				sgs.LabelVolumeName:  pvcName,
				sgs.LabelNodeName:    nodeName,
				sgs.LabelSessionMode: SessionModeRun,
			},
			Annotations: map[string]string{
				// Triggers sgs-runtime-wrapper to swap Root.Path to this PVC
				sgs.AnnotationOSVolume: pvcName,
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": nodeName,
			},
			Containers:    []corev1.Container{container},
			Volumes:       volumes,
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
}

// StopSession stops a session by deleting the pod
func StopSession(ctx context.Context, c *client.Client, nodeName, volumeName string) error {
	podName := sessionPodName(nodeName, volumeName)

	err := c.Clientset.CoreV1().Pods(c.Namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("no session found for volume %q", nodeName+"/"+volumeName)
		}
		return client.FormatK8sError(err, "stop", "session", c.Namespace)
	}

	return nil
}

// Stop stops a running session by deleting the pod (keeps PVC intact)
func Stop(ctx context.Context, c *client.Client, podName string) error {
	// Delete Pod only
	err := c.Clientset.CoreV1().Pods(c.Namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("session %q not found", podName)
		}
		return client.FormatK8sError(err, "stop", "session", c.Namespace)
	}

	return nil
}

// Delete deletes a volume (PVC only, session must be deleted first)
func Delete(ctx context.Context, c *client.Client, nodeName, volumeName string) error {
	name := pvcName(nodeName, volumeName)

	// Check if there's an active session - if so, block deletion
	mode, err := GetSessionMode(ctx, c, nodeName, volumeName)
	if err != nil {
		return client.FormatK8sError(err, "check", "session status", c.Namespace)
	}
	if mode != "" {
		return fmt.Errorf("cannot delete volume: active %s session exists. Delete the session first with: sgs delete session %s/%s", mode, nodeName, volumeName)
	}

	// Delete PVC
	pvcErr := c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if pvcErr != nil && !errors.IsNotFound(pvcErr) {
		return client.FormatK8sError(pvcErr, "delete", "volume", c.Namespace)
	}

	return nil
}

// Logs retrieves logs from a pod
func Logs(ctx context.Context, c *client.Client, podName string, opts LogsOptions) (string, error) {
	podLogOpts := &corev1.PodLogOptions{
		Follow: opts.Follow,
	}
	if opts.Tail > 0 {
		podLogOpts.TailLines = &opts.Tail
	}

	req := c.Clientset.CoreV1().Pods(c.Namespace).GetLogs(podName, podLogOpts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}
	defer stream.Close()

	var buf bytes.Buffer
	_, err = io.Copy(&buf, stream)
	if err != nil {
		return "", fmt.Errorf("failed to read logs: %w", err)
	}

	return buf.String(), nil
}

// WaitForPodReady waits for a pod to be ready
func WaitForPodReady(ctx context.Context, c *client.Client, podName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		pod, err := c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get pod: %w", err)
		}

		if pod.Status.Phase == corev1.PodRunning {
			// Check if container is ready
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.Ready {
					return nil
				}
			}
		}

		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			return fmt.Errorf("pod ended with status: %s", pod.Status.Phase)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
			// continue polling
		}
	}

	return fmt.Errorf("timeout waiting for pod to be ready")
}

// Attach attaches to a running pod with an interactive shell
func Attach(ctx context.Context, c *client.Client, podName string, stdin io.Reader, stdout, stderr io.Writer) error {
	// Run bash in the persistent root with proper environment
	// This avoids needing chroot which requires bind mounts (privileged)
	shellScript := `cd /persistent_root && export HOME=/persistent_root/root && export PATH="/persistent_root/usr/local/sbin:/persistent_root/usr/local/bin:/persistent_root/usr/sbin:/persistent_root/usr/bin:/persistent_root/sbin:/persistent_root/bin:$PATH" && exec /persistent_root/bin/bash`

	req := c.Clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(c.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: []string{"/bin/bash", "-c", shellScript},
			Stdin:   stdin != nil,
			Stdout:  stdout != nil,
			Stderr:  stderr != nil,
			TTY:     true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.Config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
	}

	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    true,
	})
}

// CopyOptions holds options for copying a volume
type CopyOptions struct {
	SrcNode   string
	SrcVolume string
	DstNode   string
	DstVolume string
}

// validateNodeAccess checks if the current workspace can access a specific node
func validateNodeAccess(ctx context.Context, c *client.Client, nodeName string) error {
	// Get current workspace info
	wsInfo, err := workspace.GetCurrent(ctx, c)
	if err != nil {
		return fmt.Errorf("failed to get workspace info: %w", err)
	}

	// Get destination node info
	node, err := c.Clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("node %s not found", nodeName)
	}

	// Get node's group label
	nodeGroup := node.Labels["node-restriction.kubernetes.io/nodegroup"]

	// Check access
	if !workspace.CanAccessNode(wsInfo.NodeGroup, nodeGroup) {
		return fmt.Errorf("workspace %q (node group: %s) cannot access node %q (node group: %s)",
			wsInfo.Name, wsInfo.NodeGroup, nodeName, nodeGroup)
	}

	return nil
}

// Copy copies contents from source volume to destination volume.
// The destination volume is created automatically with the same size and type as source.
// For same-node copies, uses a single pod. For cross-node copies, streams via tar.
func Copy(ctx context.Context, c *client.Client, opts CopyOptions) error {
	// Validate destination node is accessible from current workspace
	if err := validateNodeAccess(ctx, c, opts.DstNode); err != nil {
		return err
	}

	// Validate source volume exists
	srcInfo, err := Get(ctx, c, opts.SrcNode, opts.SrcVolume)
	if err != nil {
		return fmt.Errorf("source volume %s/%s not found", opts.SrcNode, opts.SrcVolume)
	}

	// Check if source has an active session
	srcMode, err := GetSessionMode(ctx, c, opts.SrcNode, opts.SrcVolume)
	if err != nil {
		return fmt.Errorf("failed to check source session: %w", err)
	}
	if srcMode != "" {
		return fmt.Errorf("source volume %s/%s has an active session, please delete it first", opts.SrcNode, opts.SrcVolume)
	}

	// Validate destination volume does not exist
	_, err = Get(ctx, c, opts.DstNode, opts.DstVolume)
	if err == nil {
		return fmt.Errorf("destination volume %s/%s already exists", opts.DstNode, opts.DstVolume)
	}

	// Create destination volume with same size and type
	createOpts := CreateOptions{
		NodeName:   opts.DstNode,
		VolumeName: opts.DstVolume,
		Size:       srcInfo.Size,
	}
	if srcInfo.IsOSVolume {
		// For OS volumes, we'll copy files directly instead of using image init
		// Just create a regular PVC
		createOpts.Image = "" // Don't init with image, we'll copy files
	}

	fmt.Printf("Creating destination volume %s/%s (%s)...\n", opts.DstNode, opts.DstVolume, srcInfo.Size)

	// Create destination PVC (without init)
	dstPVCName := pvcName(opts.DstNode, opts.DstVolume)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dstPVCName,
			Namespace: c.Namespace,
			Labels: map[string]string{
				sgs.LabelManagedBy:  "sgs",
				sgs.LabelNodeName:   opts.DstNode,
				sgs.LabelVolumeName: opts.DstVolume,
			},
			Annotations: map[string]string{},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(srcInfo.Size),
				},
			},
		},
	}

	// Copy OS image annotation if source is OS volume
	if srcInfo.IsOSVolume && srcInfo.Image != "" {
		pvc.Annotations[sgs.AnnotationOSImage] = srcInfo.Image
	}

	_, err = c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil {
		return client.FormatK8sError(err, "create", "destination volume", c.Namespace)
	}

	// Register cleanup for the destination PVC in case of interrupt
	cleanup.Register(func(cleanupCtx context.Context) {
		fmt.Fprint(os.Stderr, "Cleaning up destination volume...")
		if err := c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Delete(cleanupCtx, dstPVCName, metav1.DeleteOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, " failed: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, " done")
		}
	})

	// Perform copy based on whether source and destination are on the same node
	srcPVCName := pvcName(opts.SrcNode, opts.SrcVolume)

	if opts.SrcNode == opts.DstNode {
		// Same node: create single pod with both volumes
		err = copySameNode(ctx, c, opts.SrcNode, srcPVCName, dstPVCName)
	} else {
		// Different nodes: stream via tar between two pods
		err = copyCrossNode(ctx, c, opts.SrcNode, opts.DstNode, srcPVCName, dstPVCName)
	}

	if err != nil {
		// If interrupted, signal handler does cleanup - just wait and return
		if cleanup.WasInterrupted() {
			cleanup.WaitForCleanup()
			return nil
		}
		// Cleanup destination volume on failure
		cleanup.Unregister()
		fmt.Print("Copy failed, cleaning up destination volume...")
		_ = c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Delete(context.Background(), dstPVCName, metav1.DeleteOptions{})
		fmt.Println(" done")
		return err
	}

	// Success - unregister the PVC cleanup since we want to keep it
	cleanup.Unregister()

	fmt.Printf("Successfully copied %s/%s to %s/%s\n", opts.SrcNode, opts.SrcVolume, opts.DstNode, opts.DstVolume)
	return nil
}

// copySameNode copies volume contents on the same node using a single pod
func copySameNode(ctx context.Context, c *client.Client, nodeName, srcPVC, dstPVC string) error {
	podName := "copy-" + dstPVC
	fmt.Println("Copying volume contents (same node)...")

	// Create copy pod with both volumes mounted
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: c.Namespace,
			Labels: map[string]string{
				sgs.LabelManagedBy:    "sgs",
				"sgs.snucse.org/mode": "copy",
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": nodeName,
			},
			Containers: []corev1.Container{
				{
					Name:  "copy",
					Image: "busybox:latest",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("0"),
							corev1.ResourceMemory: resource.MustParse("0"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(sgs.EditCPULimit),
							corev1.ResourceMemory: resource.MustParse(sgs.EditMemoryLimit),
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "src", MountPath: "/src", ReadOnly: true},
						{Name: "dst", MountPath: "/dst"},
					},
					Command: []string{"/bin/sh", "-c"},
					Args:    []string{"cp -a /src/. /dst/ && echo 'Copy complete'"},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "src",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: srcPVC,
							ReadOnly:  true,
						},
					},
				},
				{
					Name: "dst",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: dstPVC,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	_, err := c.Clientset.CoreV1().Pods(c.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create copy pod: %w", err)
	}
	// Register cleanup for interrupt handling
	cleanup.Register(func(cleanupCtx context.Context) {
		fmt.Fprint(os.Stderr, "  Cleaning up copy pod...")
		if err := c.Clientset.CoreV1().Pods(c.Namespace).Delete(cleanupCtx, podName, metav1.DeleteOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, " failed: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, " done")
		}
	})
	defer func() {
		cleanup.Unregister()
		_ = c.Clientset.CoreV1().Pods(c.Namespace).Delete(context.Background(), podName, metav1.DeleteOptions{})
	}()

	// Wait for pod to complete
	return waitForCopyPod(ctx, c, podName, 30*time.Minute)
}

// progressWriter wraps an io.Writer and tracks bytes written
type progressWriter struct {
	writer  io.Writer
	written int64
	onWrite func(int64)
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	n, err = pw.writer.Write(p)
	pw.written += int64(n)
	if pw.onWrite != nil {
		pw.onWrite(pw.written)
	}
	return
}

// formatBytes formats bytes into human-readable format
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// copyCrossNode copies volume contents between different nodes using tar stream
func copyCrossNode(ctx context.Context, c *client.Client, srcNode, dstNode, srcPVC, dstPVC string) error {
	srcPodName := "copy-src-" + srcPVC
	dstPodName := "copy-dst-" + dstPVC

	fmt.Println("Copying volume contents (cross-node via tar stream)...")

	// Create source reader pod
	srcPod := createCopyPod(srcPodName, srcNode, srcPVC, c.Namespace, true)
	_, err := c.Clientset.CoreV1().Pods(c.Namespace).Create(ctx, srcPod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create source pod: %w", err)
	}
	// Register cleanup for interrupt handling
	cleanup.Register(func(cleanupCtx context.Context) {
		fmt.Fprint(os.Stderr, "  Cleaning up source pod...")
		if err := c.Clientset.CoreV1().Pods(c.Namespace).Delete(cleanupCtx, srcPodName, metav1.DeleteOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, " failed: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, " done")
		}
	})
	defer func() {
		cleanup.Unregister()
		_ = c.Clientset.CoreV1().Pods(c.Namespace).Delete(context.Background(), srcPodName, metav1.DeleteOptions{})
	}()

	// Create destination writer pod
	dstPod := createCopyPod(dstPodName, dstNode, dstPVC, c.Namespace, false)
	_, err = c.Clientset.CoreV1().Pods(c.Namespace).Create(ctx, dstPod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create destination pod: %w", err)
	}
	// Register cleanup for interrupt handling
	cleanup.Register(func(cleanupCtx context.Context) {
		fmt.Fprint(os.Stderr, "  Cleaning up destination pod...")
		if err := c.Clientset.CoreV1().Pods(c.Namespace).Delete(cleanupCtx, dstPodName, metav1.DeleteOptions{}); err != nil {
			fmt.Fprintf(os.Stderr, " failed: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, " done")
		}
	})
	defer func() {
		cleanup.Unregister()
		_ = c.Clientset.CoreV1().Pods(c.Namespace).Delete(context.Background(), dstPodName, metav1.DeleteOptions{})
	}()

	// Wait for both pods to be running with spinner
	fmt.Print("  Waiting for copy pods to start...")
	if err := waitForPodRunning(ctx, c, srcPodName, 5*time.Minute); err != nil {
		fmt.Println(" failed")
		return fmt.Errorf("source pod failed to start: %w", err)
	}
	if err := waitForPodRunning(ctx, c, dstPodName, 5*time.Minute); err != nil {
		fmt.Println(" failed")
		return fmt.Errorf("destination pod failed to start: %w", err)
	}
	fmt.Println(" done")

	// Stream data: tar from source | tar to destination
	fmt.Println("  Streaming data between nodes...")

	// Use a pipe to connect tar output to tar input with progress tracking
	pr, pw := io.Pipe()

	// Progress tracking
	var progressMu sync.Mutex
	var lastPrinted int64
	progress := &progressWriter{
		writer: pw,
		onWrite: func(total int64) {
			progressMu.Lock()
			defer progressMu.Unlock()
			// Update every 1MB to avoid too frequent updates
			if total-lastPrinted >= 1024*1024 {
				fmt.Printf("\r  Transferred: %s", formatBytes(total))
				lastPrinted = total
			}
		},
	}

	// Capture stderr for error messages
	var srcStderr, dstStderr bytes.Buffer

	errChan := make(chan error, 2)

	// Source: tar cf - /data
	go func() {
		defer pw.Close()
		err := execInPod(ctx, c, srcPodName, []string{"tar", "cf", "-", "-C", "/data", "."}, nil, progress, &srcStderr)
		if err != nil && srcStderr.Len() > 0 {
			err = fmt.Errorf("%w: %s", err, srcStderr.String())
		}
		errChan <- err
	}()

	// Destination: tar xf - -C /data
	go func() {
		err := execInPod(ctx, c, dstPodName, []string{"tar", "xf", "-", "-C", "/data"}, pr, nil, &dstStderr)
		if err != nil && dstStderr.Len() > 0 {
			err = fmt.Errorf("%w: %s", err, dstStderr.String())
		}
		errChan <- err
	}()

	// Wait for both to complete
	for i := 0; i < 2; i++ {
		if err := <-errChan; err != nil {
			return fmt.Errorf("copy stream failed: %w", err)
		}
	}

	// Print final progress
	fmt.Printf("\r  Transferred: %s\n", formatBytes(progress.written))

	return nil
}

// createCopyPod creates a pod for cross-node copy (long-running sleep)
func createCopyPod(name, nodeName, pvcName, namespace string, readOnly bool) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				sgs.LabelManagedBy:    "sgs",
				"sgs.snucse.org/mode": "copy",
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": nodeName,
			},
			Containers: []corev1.Container{
				{
					Name:  "copy",
					Image: "busybox:latest",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("0"),
							corev1.ResourceMemory: resource.MustParse("0"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(sgs.EditCPULimit),
							corev1.ResourceMemory: resource.MustParse(sgs.EditMemoryLimit),
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "data", MountPath: "/data", ReadOnly: readOnly},
					},
					Command: []string{"/bin/sh", "-c"},
					Args:    []string{"sleep 3600"}, // Stay alive for exec
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
							ReadOnly:  readOnly,
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
}

// waitForCopyPod waits for the copy pod to complete
func waitForCopyPod(ctx context.Context, c *client.Client, podName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	// Spinner characters for progress indication
	spinChars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinIdx := 0
	startTime := time.Now()

	for time.Now().Before(deadline) {
		pod, err := c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			fmt.Print("\r                              \r") // Clear spinner line
			return fmt.Errorf("failed to get copy pod: %w", err)
		}

		elapsed := time.Since(startTime).Round(time.Second)
		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			fmt.Printf("\r  Copying... done (%s)       \n", elapsed)
			return nil
		case corev1.PodFailed:
			fmt.Print("\r                              \r") // Clear spinner line
			return fmt.Errorf("copy pod failed")
		case corev1.PodPending:
			// Check for errors
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil {
					if cs.State.Waiting.Reason == "ImagePullBackOff" || cs.State.Waiting.Reason == "ErrImagePull" {
						fmt.Print("\r                              \r") // Clear spinner line
						return fmt.Errorf("failed to pull image: %s", cs.State.Waiting.Message)
					}
				}
			}
			fmt.Printf("\r  %s Waiting for copy pod to start... (%s)", spinChars[spinIdx], elapsed)
		case corev1.PodRunning:
			fmt.Printf("\r  %s Copying... (%s)", spinChars[spinIdx], elapsed)
		}
		spinIdx = (spinIdx + 1) % len(spinChars)

		select {
		case <-ctx.Done():
			fmt.Print("\r                              \r") // Clear spinner line
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	fmt.Print("\r                              \r") // Clear spinner line
	return fmt.Errorf("timeout waiting for copy to complete")
}

// waitForPodRunning waits for a pod to be running
func waitForPodRunning(ctx context.Context, c *client.Client, podName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Check context first
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		pod, err := c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("failed to get pod: %w", err)
		}

		if pod.Status.Phase == corev1.PodRunning {
			return nil
		}
		if pod.Status.Phase == corev1.PodFailed {
			return fmt.Errorf("pod failed")
		}

		// Check for pending errors
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil {
				if cs.State.Waiting.Reason == "ImagePullBackOff" || cs.State.Waiting.Reason == "ErrImagePull" {
					return fmt.Errorf("failed to pull image: %s", cs.State.Waiting.Message)
				}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	return fmt.Errorf("timeout waiting for pod to start")
}

// execInPod executes a command in a pod with stdin/stdout/stderr
func execInPod(ctx context.Context, c *client.Client, podName string, command []string, stdin io.Reader, stdout, stderr io.Writer) error {
	req := c.Clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(c.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: command,
			Stdin:   stdin != nil,
			Stdout:  stdout != nil,
			Stderr:  stderr != nil,
			TTY:     false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.Config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("failed to create executor: %w", err)
	}

	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Tty:    false,
	})
}
