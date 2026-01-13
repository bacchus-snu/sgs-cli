package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/bacchus-snu/sgs-cli/internal/volume"
	"github.com/spf13/cobra"
)

const (
	defaultImage = "nvidia/cuda:12.6.1-cudnn-devel-ubuntu24.04"
)

var (
	createSize    string
	createImage   string
	sessionGPUs   int
	sessionCmd    []string
	sessionMounts []string
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a resource",
}

var createVolumeCmd = &cobra.Command{
	Use:   "volume <node-name>/<volume-name>",
	Short: "Create a new volume",
	Long: `Create a new persistent volume on a specific node.

There are two types of volumes:
  - OS Volume: Created with --image flag. Can be used with sessions.
  - Data Volume: Created without --image. Used for data storage only.

The volume name format is: <node-name>/<volume-name>

The --image flag can be used in two ways:
  - --image <custom-image>: Use a specific container image
  - --image (without value): Use the default image (nvidia/cuda:12.6.1-cudnn-devel-ubuntu24.04)

Examples:
  # Create an OS volume with default image
  sgs create volume ferrari/os-volume --image

  # Create an OS volume with custom image
  sgs create volume ferrari/os-volume --image pytorch/pytorch:2.0.0-cuda11.7-cudnn8-devel

  # Create a data volume (no image, storage only)
  sgs create volume ferrari/data-vol --size 100Gi

  # Create with custom size
  sgs create volume ferrari/os-volume --image --size 50Gi`,
	Args: cobra.ExactArgs(1),
	Run:  runCreateVolume,
}

var createSessionCmd = &cobra.Command{
	Use:   "session <node>/<volume>[/<number>]",
	Short: "Create a session on an OS volume",
	Long: `Create a session on an OS volume.

Without --gpu flag: Creates an edit session (session 0) for interactive use.
  - Limited resources (4 CPU, 16GiB memory)
  - Attaches to shell immediately
  - Only one edit session per volume
  - Session number is always 0 (cannot be specified)

With --gpu flag: Creates a run session for GPU workloads.
  - Requires --command flag
  - Session number can be specified (must be >= 1), or auto-assigned
  - CPU/memory automatically calculated based on GPU count

You can mount additional volumes using the --mount flag.

Examples:
  # Start an edit session (session 0)
  sgs create session ferrari/os-volume

  # Start an edit session with mounted data volume
  sgs create session ferrari/os-volume --mount ferrari/data-vol:/data

  # Start a run session with 1 GPU (auto-assign session number)
  sgs create session ferrari/os-volume --gpu 1 --command python train.py

  # Start a run session with specific session number
  sgs create session ferrari/os-volume/3 --gpu 2 --command "python train.py"`,
	Args: cobra.ExactArgs(1),
	Run:  runCreateSession,
}

func init() {
	createCmd.AddCommand(createVolumeCmd)
	createCmd.AddCommand(createSessionCmd)

	createVolumeCmd.Flags().StringVar(&createSize, "size", "", "Storage size (default: 10Gi)")
	createVolumeCmd.Flags().StringVar(&createImage, "image", "", "Container image for OS volume (default: "+defaultImage+")")
	createVolumeCmd.Flags().Lookup("image").NoOptDefVal = defaultImage

	createSessionCmd.Flags().IntVar(&sessionGPUs, "gpu", 0, "Number of GPUs (0 for edit mode, 1+ for run mode)")
	createSessionCmd.Flags().StringArrayVar(&sessionCmd, "command", nil, "Command to run (required for run mode)")
	createSessionCmd.Flags().StringArrayVar(&sessionMounts, "mount", nil, "Mount volumes (<node>/<volume>:<path>)")
}

func runCreateVolume(cmd *cobra.Command, args []string) {
	// Parse node-name/volume-name
	parts := strings.SplitN(args[0], "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		exitWithError("invalid volume name format, expected: <node-name>/<volume-name>", nil)
	}
	nodeName := parts[0]
	volumeName := parts[1]

	ctx := context.Background()

	k8sClient, err := client.New()
	if err != nil {
		exitWithError("failed to create client", err)
	}

	opts := volume.CreateOptions{
		NodeName:   nodeName,
		VolumeName: volumeName,
		Size:       createSize,
		Image:      createImage,
	}

	volumeType := "data"
	if createImage != "" {
		volumeType = "OS"
	}

	fmt.Printf("Creating %s volume %s on %s...\n", volumeType, volumeName, nodeName)

	if err := volume.Create(ctx, k8sClient, opts); err != nil {
		exitWithError("failed to create volume", err)
	}

	volumePath := nodeName + "/" + volumeName
	fmt.Printf("Volume %s created successfully\n", volumePath)
	if createImage != "" {
		fmt.Printf("Use 'sgs create session %s' to start an edit session\n", volumePath)
	}
}

func runCreateSession(cmd *cobra.Command, args []string) {
	sessionPath := args[0]

	// Parse path: <node>/<volume>[/<number>]
	parts := strings.Split(sessionPath, "/")
	if len(parts) < 2 || len(parts) > 3 {
		exitWithError("invalid session path format, expected: <node>/<volume>[/<number>]", nil)
	}

	nodeName := parts[0]
	volumeName := parts[1]
	sessionNumber := -1 // -1 means auto-assign

	if len(parts) == 3 {
		var err error
		sessionNumber, err = strconv.Atoi(parts[2])
		if err != nil {
			exitWithError("invalid session number", err)
		}
	}

	if nodeName == "" || volumeName == "" {
		exitWithError("invalid session path format, expected: <node>/<volume>[/<number>]", nil)
	}

	// Parse mounts
	mounts, err := parseMounts(sessionMounts)
	if err != nil {
		exitWithError("invalid mount format", err)
	}

	ctx := context.Background()

	k8sClient, err := client.New()
	if err != nil {
		exitWithError("failed to create client", err)
	}

	if sessionGPUs == 0 {
		// Edit mode (session 0)
		if sessionNumber != -1 && sessionNumber != 0 {
			exitWithError("edit session (no --gpu) must be session 0", nil)
		}
		runEditSession(ctx, k8sClient, nodeName, volumeName, mounts)
	} else {
		// Run mode (session 1+)
		if len(sessionCmd) == 0 {
			exitWithError("--command is required when using --gpu", nil)
		}
		if sessionNumber != -1 && sessionNumber < 1 {
			exitWithError("run session (with --gpu) must have session number >= 1", nil)
		}
		runGPUSession(ctx, k8sClient, nodeName, volumeName, sessionGPUs, sessionCmd, mounts, sessionNumber)
	}
}

func runEditSession(ctx context.Context, k8sClient *client.Client, nodeName, volumeName string, mounts []volume.MountOption) {
	opts := volume.EditOptions{
		NodeName:   nodeName,
		VolumeName: volumeName,
		Mounts:     mounts,
	}

	fmt.Printf("Starting edit session for %s/%s...\n", nodeName, volumeName)

	result, err := volume.Edit(ctx, k8sClient, opts)
	if err != nil {
		exitWithError("failed to start edit session", err)
	}

	podName := result.PodName

	if result.Existing {
		fmt.Println("Reattaching to existing edit session...")
	}

	// Wait for pod to be ready
	fmt.Println("Waiting for pod to be ready...")
	if err := volume.WaitForPodReady(ctx, k8sClient, podName, 5*time.Minute); err != nil {
		exitWithError("pod failed to become ready", err)
	}

	fmt.Printf("Attaching to shell (use 'sgs delete session %s/%s' to delete)...\n", nodeName, volumeName)

	// Attach to the pod
	if err := volume.Attach(ctx, k8sClient, podName, os.Stdin, os.Stdout, os.Stderr); err != nil {
		exitWithError("failed to attach to pod", err)
	}
}

func runGPUSession(ctx context.Context, k8sClient *client.Client, nodeName, volumeName string, gpus int, command []string, mounts []volume.MountOption, requestedNumber int) {
	opts := volume.RunOptions{
		NodeName:      nodeName,
		VolumeName:    volumeName,
		GPUs:          gpus,
		Command:       command,
		Mounts:        mounts,
		SessionNumber: requestedNumber, // -1 means auto-assign
	}

	fmt.Printf("Starting run session with %d GPU(s) on %s/%s...\n", gpus, nodeName, volumeName)

	result, err := volume.Run(ctx, k8sClient, opts)
	if err != nil {
		exitWithError("failed to start run session", err)
	}

	fmt.Printf("Run session started: %s/%s/%d\n", nodeName, volumeName, result.SessionNumber)
	fmt.Printf("Use 'sgs logs %s/%s/%d' to view output\n", nodeName, volumeName, result.SessionNumber)
	fmt.Printf("Use 'sgs delete session %s/%s/%d' to stop\n", nodeName, volumeName, result.SessionNumber)
}

// parseMounts parses mount options from strings like "node/volume:/path"
func parseMounts(mountStrs []string) ([]volume.MountOption, error) {
	var mounts []volume.MountOption
	for _, m := range mountStrs {
		parts := strings.SplitN(m, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid mount format '%s', expected <node>/<volume>:<path>", m)
		}
		// Parse node/volume to get PVC name
		nodeName, volName, err := volume.ParseVolumePath(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid mount volume '%s': %w", parts[0], err)
		}
		mounts = append(mounts, volume.MountOption{
			SourceVolume: nodeName + "-" + volName, // PVC name format
			MountPath:    parts[1],
		})
	}
	return mounts, nil
}