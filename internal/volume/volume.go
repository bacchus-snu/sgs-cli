package volume

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	// Label keys for SGS-managed resources (hardcoded)
	LabelManagedBy     = "app.kubernetes.io/managed-by"
	LabelNodeName      = "sgs.bacchus.io/node-name"
	LabelVolumeName    = "sgs.bacchus.io/volume-name"
	LabelSessionNumber = "sgs.bacchus.io/session-number"

	// Annotation for selected node (set by k8s storage provisioner)
	AnnotationSelectedNode = "volume.kubernetes.io/selected-node"
	// Annotation for OS volume image
	AnnotationOSImage = "sgs.bacchus.io/os-image"

	// Hardcoded values
	DefaultImage       = "nvidia/cuda:12.6.1-cudnn-devel-ubuntu24.04"
	DefaultStorageSize = "10Gi"

	// Edit mode defaults
	EditCPULimit    = "4"
	EditMemoryLimit = "16Gi"
)

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
	NodeName      string
	VolumeName    string
	GPUs          int
	Command       []string      // Command to run (required)
	Mounts        []MountOption // Additional volumes to mount
	SessionNumber int           // Requested session number (-1 for auto-assign)
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
	// List ALL PVCs in namespace
	pvcs, err := c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list PVCs: %w", err)
	}

	var volumes []VolumeInfo
	for _, pvc := range pvcs.Items {
		// Get node from selected-node annotation, fallback to label
		nodeName := pvc.Annotations[AnnotationSelectedNode]
		if nodeName == "" {
			nodeName = pvc.Labels[LabelNodeName]
		}

		// Get volume name from label (PVC name is <node>-<volume>)
		volumeName := pvc.Labels[LabelVolumeName]
		if volumeName == "" {
			// Fallback: try to extract from PVC name (for backwards compatibility)
			volumeName = pvc.Name
		}

		// Check if this is an OS volume (has image annotation)
		osImage := pvc.Annotations[AnnotationOSImage]
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
	pvc, err := c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Get(ctx, pvcName(nodeName, volumeName), metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("volume not found: %w", err)
	}

	// Check if this is an OS volume (has image annotation)
	osImage := pvc.Annotations[AnnotationOSImage]
	isOSVolume := osImage != ""

	// Check if there's an associated pod
	pod, err := c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, pvcName(nodeName, volumeName), metav1.GetOptions{})
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

// Create creates a new volume
// If Image is empty, creates a data volume (PVC only, for storage)
// If Image is set, creates an OS volume (PVC + init pod that copies base files)
func Create(ctx context.Context, c *client.Client, opts CreateOptions) error {
	name := pvcName(opts.NodeName, opts.VolumeName)

	// Set defaults
	if opts.Size == "" {
		opts.Size = DefaultStorageSize
	}

	// Create labels
	labels := map[string]string{
		LabelManagedBy:  "sgs",
		LabelNodeName:   opts.NodeName,
		LabelVolumeName: opts.VolumeName,
	}

	// Create annotations
	annotations := make(map[string]string)
	if opts.Image != "" {
		// OS volume - store image in annotation
		annotations[AnnotationOSImage] = opts.Image
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
		return fmt.Errorf("failed to create PVC: %w", err)
	}

	// For OS volumes, create an init pod to bind PVC and copy base files
	if opts.Image != "" {
		initPod := createInitPodSpec(name, opts.NodeName, opts.Image, c.Namespace)
		podName := initPod.Name
		_, err = c.Clientset.CoreV1().Pods(c.Namespace).Create(ctx, initPod, metav1.CreateOptions{})
		if err != nil {
			// Cleanup PVC if pod creation fails
			_ = c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Delete(ctx, name, metav1.DeleteOptions{})
			return fmt.Errorf("failed to create init pod: %w", err)
		}

		// Wait for init pod to complete or fail
		if err := waitForInitPod(ctx, c, podName, 5*time.Minute); err != nil {
			// Cleanup on failure
			_ = c.Clientset.CoreV1().Pods(c.Namespace).Delete(ctx, podName, metav1.DeleteOptions{})
			_ = c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Delete(ctx, name, metav1.DeleteOptions{})
			return fmt.Errorf("init failed: %w", err)
		}

		// Delete init pod after successful completion
		_ = c.Clientset.CoreV1().Pods(c.Namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	}

	return nil
}

// waitForInitPod waits for the init pod to complete successfully or fail
func waitForInitPod(ctx context.Context, c *client.Client, podName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		pod, err := c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get init pod: %w", err)
		}

		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			return nil
		case corev1.PodFailed:
			// Get reason from container status
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
					return fmt.Errorf("init pod failed: %s", cs.State.Terminated.Reason)
				}
				if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
					return fmt.Errorf("init pod failed: %s - %s", cs.State.Waiting.Reason, cs.State.Waiting.Message)
				}
			}
			return fmt.Errorf("init pod failed")
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

	return fmt.Errorf("timeout waiting for init pod to complete")
}

// createInitPodSpec creates a pod that initializes the OS volume (copies base files, then exits)
func createInitPodSpec(pvcName, nodeName, image, namespace string) *corev1.Pod {
	podName := "init-" + pvcName

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				LabelManagedBy:        "sgs",
				"sgs.bacchus.io/mode": "init",
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": nodeName,
			},
			Containers: []corev1.Container{
				{
					Name:  "init",
					Image: image,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "rootfs-storage",
							MountPath: "/persistent_root",
						},
					},
					Command: []string{"/bin/bash", "-c"},
					Args:    []string{getInitCopyScript()},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "rootfs-storage",
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

// getInitCopyScript returns script that only copies base files (no mounts, no privileged needed)
func getInitCopyScript() string {
	return `set -e
TARGET="/persistent_root"

if [ ! -d "$TARGET/etc" ]; then
  echo "Copying base system files..."
  for dir in bin etc lib lib64 sbin usr var root; do
    if [ -d "/$dir" ]; then cp -a "/$dir" "$TARGET/"; fi
  done
  mkdir -p "$TARGET/dev" "$TARGET/proc" "$TARGET/sys" "$TARGET/tmp"
  chmod 1777 "$TARGET/tmp"
  mkdir -p "$TARGET/usr/local/nvidia" "$TARGET/usr/local/vgpu"
  mkdir -p "$TARGET/usr/lib/firmware/nvidia"
  mkdir -p "$TARGET/run/nvidia-persistenced"
  mkdir -p "$TARGET/tmp/vgpulock"
  echo "Base system files copied successfully"
else
  echo "Base system files already exist, skipping copy"
fi

echo "Init complete, volume is ready"
`
}

// GetPVCInfo retrieves PVC info including OS image
func GetPVCInfo(ctx context.Context, c *client.Client, nodeName, volumeName string) (osImage string, err error) {
	pvc, err := c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Get(ctx, pvcName(nodeName, volumeName), metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("volume not found: %w", err)
	}
	return pvc.Annotations[AnnotationOSImage], nil
}

// GetNodeResources returns total CPU cores, memory bytes, and GPU count for a node
func GetNodeResources(ctx context.Context, c *client.Client, nodeName string) (cpuCores int64, memoryBytes int64, gpuCount int64, err error) {
	node, err := c.Clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to get node: %w", err)
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
// Format: <node>-<volume>-<number>
// Session 0 is edit mode, sessions 1+ are run mode
func sessionPodName(nodeName, volumeName string, number int) string {
	return fmt.Sprintf("%s-%s-%d", nodeName, volumeName, number)
}

// findNextRunSessionNumber finds the next available number for run mode (starting from 1)
func findNextRunSessionNumber(ctx context.Context, c *client.Client, nodeName, volumeName string) (int, error) {
	// Run sessions start from 1 (0 is edit session)
	for i := 1; i < 1000; i++ {
		name := sessionPodName(nodeName, volumeName, i)
		_, err := c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return i, nil
		}
		if err != nil {
			return 0, fmt.Errorf("failed to check pod: %w", err)
		}
	}

	return 0, fmt.Errorf("too many run sessions for volume %s/%s", nodeName, volumeName)
}

// EditResult contains the result of starting an edit session
type EditResult struct {
	PodName  string
	Existing bool // true if attached to existing session
}

// RunResult contains the result of starting a run session
type RunResult struct {
	PodName       string
	SessionNumber int
}

// Edit starts an edit session for an OS volume (no GPU, limited CPU/memory)
// Returns the pod name and whether it's an existing session
func Edit(ctx context.Context, c *client.Client, opts EditOptions) (*EditResult, error) {
	podName := sessionPodName(opts.NodeName, opts.VolumeName, 0)
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

	// Check if edit pod already exists
	existingPod, err := c.Clientset.CoreV1().Pods(c.Namespace).Get(ctx, podName, metav1.GetOptions{})
	if err == nil {
		// Pod exists - check if it's still usable
		if existingPod.Status.Phase == corev1.PodRunning || existingPod.Status.Phase == corev1.PodPending {
			return &EditResult{PodName: podName, Existing: true}, nil
		}
		// Pod exists but is terminated - delete it first
		if err := c.Clientset.CoreV1().Pods(c.Namespace).Delete(ctx, podName, metav1.DeleteOptions{}); err != nil {
			return nil, fmt.Errorf("failed to cleanup terminated edit pod: %w", err)
		}
		// Wait a moment for deletion
		time.Sleep(time.Second)
	} else if !errors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to check existing pod: %w", err)
	}

	// Create pod with edit mode resources
	pod := createEditPodSpec(podName, pvc, opts.NodeName, opts.VolumeName, osImage, opts.Mounts, c.Namespace)

	_, err = c.Clientset.CoreV1().Pods(c.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create Pod: %w", err)
	}

	return &EditResult{PodName: podName, Existing: false}, nil
}

// Run starts a GPU session for an OS volume
// Returns the run result with pod name and session number
func Run(ctx context.Context, c *client.Client, opts RunOptions) (*RunResult, error) {
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

	// Determine session number
	var sessionNumber int
	if opts.SessionNumber >= 1 {
		// Use requested session number
		sessionNumber = opts.SessionNumber
	} else {
		// Auto-assign: find next available session number (run sessions start from 1)
		sessionNumber, err = findNextRunSessionNumber(ctx, c, opts.NodeName, opts.VolumeName)
		if err != nil {
			return nil, err
		}
	}
	podName := sessionPodName(opts.NodeName, opts.VolumeName, sessionNumber)

	// Get node resources to calculate CPU/memory limits
	totalCPU, totalMemory, totalGPU, err := GetNodeResources(ctx, c, opts.NodeName)
	if err != nil {
		return nil, err
	}

	if totalGPU == 0 {
		return nil, fmt.Errorf("node %s has no GPUs available", opts.NodeName)
	}

	// Calculate resources per GPU: (7/8) of resources divided by total GPUs
	// CPU per GPU = (7 * totalCPU) / (8 * totalGPU)
	// Memory per GPU = (7 * totalMemory) / (8 * totalGPU)
	cpuPerGPU := (7 * totalCPU) / (8 * totalGPU)
	memPerGPU := (7 * totalMemory) / (8 * totalGPU)

	cpuLimit := cpuPerGPU * int64(opts.GPUs)
	memLimit := memPerGPU * int64(opts.GPUs)

	// Create pod with GPU resources
	pod := createRunPodSpec(podName, pvc, opts.NodeName, opts.VolumeName, osImage, sessionNumber, opts.GPUs, cpuLimit, memLimit, opts.Command, opts.Mounts, c.Namespace)

	_, err = c.Clientset.CoreV1().Pods(c.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create Pod: %w", err)
	}

	return &RunResult{PodName: podName, SessionNumber: sessionNumber}, nil
}

// createEditPodSpec creates a pod for edit mode (no GPU, limited resources)
func createEditPodSpec(podName, pvcName, nodeName, volumeName, image string, mounts []MountOption, namespace string) *corev1.Pod {
	// Build volume mounts and volumes
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "rootfs-storage",
			MountPath: "/persistent_root",
		},
	}
	volumes := []corev1.Volume{
		{
			Name: "rootfs-storage",
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

	// Edit mode: 4 CPU limit, 16Gi memory limit, no requests
	zero := resource.MustParse("0")

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				LabelManagedBy:     "sgs",
				LabelVolumeName:    pvcName,
				LabelNodeName:      nodeName,
				LabelSessionNumber: "0",
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": nodeName,
			},
			Containers: []corev1.Container{
				{
					Name:  "work-node",
					Image: image,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    zero,
							corev1.ResourceMemory: zero,
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(EditCPULimit),
							corev1.ResourceMemory: resource.MustParse(EditMemoryLimit),
						},
					},
					VolumeMounts: volumeMounts,
					Command:      []string{"/bin/bash", "-c"},
					Args:         []string{getInitScript()},
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
func createRunPodSpec(podName, pvcName, nodeName, volumeName, image string, sessionNumber, gpus int, cpuLimit, memLimit int64, command []string, mounts []MountOption, namespace string) *corev1.Pod {
	// Build volume mounts and volumes
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "rootfs-storage",
			MountPath: "/persistent_root",
		},
	}
	volumes := []corev1.Volume{
		{
			Name: "rootfs-storage",
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

	// Run mode: calculated CPU/memory limits based on GPU count, no requests
	zero := resource.MustParse("0")

	// Build the init script with the user command
	initScript := getRunInitScript(command)

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				LabelManagedBy:     "sgs",
				LabelVolumeName:    pvcName,
				LabelNodeName:      nodeName,
				LabelSessionNumber: fmt.Sprintf("%d", sessionNumber),
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": nodeName,
			},
			Containers: []corev1.Container{
				{
					Name:  "work-node",
					Image: image,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    zero,
							corev1.ResourceMemory: zero,
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:                    *resource.NewQuantity(cpuLimit, resource.DecimalSI),
							corev1.ResourceMemory:                 *resource.NewQuantity(memLimit, resource.BinarySI),
							corev1.ResourceName("nvidia.com/gpu"): resource.MustParse(fmt.Sprintf("%d", gpus)),
						},
					},
					VolumeMounts: volumeMounts,
					Command:      []string{"/bin/bash", "-c"},
					Args:         []string{initScript},
				},
			},
			Volumes:       volumes,
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
}

// getInitScript returns the initialization script for edit mode (keeps running)
// Uses proot for user-space chroot that doesn't require privileges
func getInitScript() string {
	return `set -e
TARGET="/persistent_root"

# Initialize persistent root if not done
if [ ! -d "$TARGET/etc" ]; then
  echo "Initializing persistent root filesystem..."
  for dir in bin etc lib lib64 sbin usr var opt root home; do
    if [ -d "/$dir" ]; then cp -a "/$dir" "$TARGET/"; fi
  done
  mkdir -p "$TARGET/dev" "$TARGET/proc" "$TARGET/sys" "$TARGET/tmp" "$TARGET/run"
  chmod 1777 "$TARGET/tmp"
fi

# Create symlinks for device nodes (these point to host /dev via /persistent_root/../dev)
# We'll set up PATH to use the persistent root
cd "$TARGET"

# Set up environment to use persistent root binaries and libs
export PATH="$TARGET/usr/local/sbin:$TARGET/usr/local/bin:$TARGET/usr/sbin:$TARGET/usr/bin:$TARGET/sbin:$TARGET/bin:$PATH"
export LD_LIBRARY_PATH="$TARGET/usr/local/lib:$TARGET/usr/lib/x86_64-linux-gnu:$TARGET/lib/x86_64-linux-gnu:$LD_LIBRARY_PATH"
export HOME="$TARGET/root"

# Keep container running
tail -f /dev/null`
}

// getRunInitScript returns the initialization script for run mode (executes command then exits)
func getRunInitScript(command []string) string {
	// Escape the command for shell
	escapedCmd := ""
	for i, arg := range command {
		if i > 0 {
			escapedCmd += " "
		}
		// Simple escaping: wrap in single quotes and escape existing single quotes
		escapedCmd += "'" + escapeShellArg(arg) + "'"
	}

	return fmt.Sprintf(`set -e
TARGET="/persistent_root"

# Initialize persistent root if not done
if [ ! -d "$TARGET/etc" ]; then
  echo "Initializing persistent root filesystem..."
  for dir in bin etc lib lib64 sbin usr var opt root home; do
    if [ -d "/$dir" ]; then cp -a "/$dir" "$TARGET/"; fi
  done
  mkdir -p "$TARGET/dev" "$TARGET/proc" "$TARGET/sys" "$TARGET/tmp" "$TARGET/run"
  chmod 1777 "$TARGET/tmp"
fi

# Set up environment to use persistent root binaries and libs
cd "$TARGET"
export PATH="$TARGET/usr/local/sbin:$TARGET/usr/local/bin:$TARGET/usr/sbin:$TARGET/usr/bin:$TARGET/sbin:$TARGET/bin:$PATH"
export LD_LIBRARY_PATH="$TARGET/usr/local/lib:$TARGET/usr/lib/x86_64-linux-gnu:$TARGET/lib/x86_64-linux-gnu:$LD_LIBRARY_PATH"
export HOME="$TARGET/root"

# Execute the user command
%s`, escapedCmd)
}

// escapeShellArg escapes single quotes in a shell argument
func escapeShellArg(s string) string {
	result := ""
	for _, c := range s {
		if c == '\'' {
			result += "'\"'\"'"
		} else {
			result += string(c)
		}
	}
	return result
}

// StopSession stops a session by deleting the pod
func StopSession(ctx context.Context, c *client.Client, nodeName, volumeName string, sessionNumber int) error {
	podName := sessionPodName(nodeName, volumeName, sessionNumber)

	err := c.Clientset.CoreV1().Pods(c.Namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("no session %d found for volume '%s/%s'", sessionNumber, nodeName, volumeName)
		}
		return fmt.Errorf("failed to stop session: %w", err)
	}

	return nil
}

// Stop stops a running session by deleting the pod (keeps PVC intact)
func Stop(ctx context.Context, c *client.Client, podName string) error {
	// Delete Pod only
	err := c.Clientset.CoreV1().Pods(c.Namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("no running session found: pod '%s' not found", podName)
		}
		return fmt.Errorf("failed to stop session: %w", err)
	}

	return nil
}

// Delete deletes a volume (Pod + PVC)
func Delete(ctx context.Context, c *client.Client, nodeName, volumeName string) error {
	name := pvcName(nodeName, volumeName)

	// Delete all session pods (0 is edit, 1+ are run)
	for i := 0; i < 100; i++ {
		podName := sessionPodName(nodeName, volumeName, i)
		err := c.Clientset.CoreV1().Pods(c.Namespace).Delete(ctx, podName, metav1.DeleteOptions{})
		if errors.IsNotFound(err) {
			if i > 0 {
				break // No more run sessions
			}
			continue // Session 0 might not exist, continue checking
		}
		if err != nil {
			return fmt.Errorf("failed to delete session Pod: %w", err)
		}
	}

	// Delete PVC
	pvcErr := c.Clientset.CoreV1().PersistentVolumeClaims(c.Namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if pvcErr != nil && !errors.IsNotFound(pvcErr) {
		return fmt.Errorf("failed to delete PVC: %w", pvcErr)
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
