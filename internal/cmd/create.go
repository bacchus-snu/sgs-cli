package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bacchus-snu/sgs-cli/internal/cleanup"
	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/bacchus-snu/sgs-cli/internal/volume"
	"github.com/spf13/cobra"
)

var (
	createSize  string
	createImage string

	// Session flags
	sessionRunMode bool  // --run flag
	sessionGPUNum  int   // --gpu-num flag
	sessionGPUMem  int64 // --gpu-mem flag (MiB)
	sessionPinCPU  int64 // --pin-cpu flag (cores)
	sessionPinMem  int64 // --pin-mem flag (bytes)
	sessionCmd     []string
	sessionMounts  []string
	sessionAttach  bool // --attach flag
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a resource",
}

var createVolumeCmd = &cobra.Command{
	Use:     "volume <node-name>/<volume-name>",
	Aliases: []string{"volumes"},
	Short:   "Create a new volume",
	Long: `Create a new persistent volume on a specific node.

There are two types of volumes:
  - OS Volume: Created with --image flag. Can be used with sessions.
  - Data Volume: Created without --image. Used for data storage only.

The volume name format is: <node-name>/<volume-name>

The --image flag can be used in two ways:
  - --image <custom-image>: Use a specific container image
  - --image (without value): Use the default image (nvidia/cuda:12.5.0-base-ubuntu22.04)

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
	Use:     "session <node>/<volume>",
	Aliases: []string{"sessions"},
	Short:   "Create a session on an OS volume",
	Long: `Create a session on an OS volume.

By default (or with --edit): Creates an edit session for interactive use.
  - Limited resources (4 CPU, 16GiB memory)
  - GPU memory set to 0 (HAMi: drivers accessible, CUDA blocked)
  - Use --attach to attach to shell immediately

With --run flag: Creates a run session for GPU workloads.
  - Requires --gpu-num and --gpu-mem flags
  - Optional --command flag for batch execution
  - CPU/memory automatically calculated based on GPU count
  - Use --pin-cpu and --pin-mem to pin resources

You can mount additional volumes using the --mount flag.

Examples:
  # Start an edit session
  sgs create session ferrari/os-volume

  # Start an edit session and attach immediately
  sgs create session ferrari/os-volume --attach

  # Start an edit session with mounted data volume
  sgs create session ferrari/os-volume --mount ferrari/data-vol:/data

  # Start a run session with GPU (interactive)
  sgs create session ferrari/os-volume --run --gpu-num 1 --gpu-mem 8192 --attach

  # Start a run session with batch command
  sgs create session ferrari/os-volume --run --gpu-num 2 --gpu-mem 16384 --command "python train.py"

  # Start a run session with pinned resources
  sgs create session ferrari/os-volume --run --gpu-num 1 --gpu-mem 8192 --pin-cpu 8 --pin-mem 34359738368`,
	Args: cobra.ExactArgs(1),
	Run:  runCreateSession,
}

func init() {
	createCmd.AddCommand(createVolumeCmd)
	createCmd.AddCommand(createSessionCmd)

	createVolumeCmd.Flags().StringVar(&createSize, "size", "", "Storage size (default: 10Gi)")
	createVolumeCmd.Flags().StringVar(&createImage, "image", "", "Container image for OS volume (default: "+volume.DefaultImage+")")
	createVolumeCmd.Flags().Lookup("image").NoOptDefVal = volume.DefaultImage

	createSessionCmd.Flags().BoolVar(&sessionRunMode, "run", false, "Create a run session (with GPU)")
	createSessionCmd.Flags().BoolVar(&sessionAttach, "attach", false, "Attach to the session after creation")
	createSessionCmd.Flags().IntVar(&sessionGPUNum, "gpu-num", 0, "Number of GPUs (required for run mode)")
	createSessionCmd.Flags().Int64Var(&sessionGPUMem, "gpu-mem", 0, "GPU memory in MiB (required for run mode)")
	createSessionCmd.Flags().Int64Var(&sessionPinCPU, "pin-cpu", 0, "Pin CPU cores (0 = no pinning)")
	createSessionCmd.Flags().Int64Var(&sessionPinMem, "pin-mem", 0, "Pin memory in bytes (0 = no pinning)")
	createSessionCmd.Flags().StringArrayVar(&sessionCmd, "command", nil, "Command to run (for batch execution)")
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

	// Use InterruptibleContext for cleanup on interrupt
	ctx, cancel := cleanup.InterruptibleContext(context.Background())
	defer cancel()

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
		exitWithError("", err)
	}

	volumePath := nodeName + "/" + volumeName
	fmt.Printf("Volume %s created successfully\n", volumePath)
	if createImage != "" {
		fmt.Printf("Use 'sgs create session %s' to start an edit session\n", volumePath)
	}
}

func runCreateSession(cmd *cobra.Command, args []string) {
	sessionPath := args[0]

	// Parse path: <node>/<volume>
	nodeName, volumeName, err := volume.ParseVolumePath(sessionPath)
	if err != nil {
		exitWithError("invalid session path format, expected: <node>/<volume>", nil)
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

	// Check for existing session
	existingMode, err := volume.GetSessionMode(ctx, k8sClient, nodeName, volumeName)
	if err != nil {
		exitWithError("", err)
	}

	requestedMode := volume.SessionModeEdit
	if sessionRunMode {
		requestedMode = volume.SessionModeRun
	}

	if sessionRunMode {
		// Run mode validations
		if sessionGPUNum <= 0 && sessionGPUMem <= 0 {
			exitWithError("--gpu-num and --gpu-mem are required for run mode", nil)
		}
		if sessionGPUNum <= 0 {
			exitWithError("--gpu-num is required for run mode", nil)
		}
		if sessionGPUMem <= 0 {
			exitWithError("--gpu-mem is required for run mode", nil)
		}
	} else {
		// Edit mode validations - reject GPU-related flags
		if sessionGPUNum > 0 {
			exitWithError("--gpu-num is only valid for run mode (use --run flag)", nil)
		}
		if sessionGPUMem > 0 {
			exitWithError("--gpu-mem is only valid for run mode (use --run flag)", nil)
		}
		if sessionPinCPU > 0 {
			exitWithError("--pin-cpu is only valid for run mode (use --run flag)", nil)
		}
		if sessionPinMem > 0 {
			exitWithError("--pin-mem is only valid for run mode (use --run flag)", nil)
		}
		if len(sessionCmd) > 0 {
			exitWithError("--command is only valid for run mode (use --run flag)", nil)
		}
	}

	if existingMode != "" {
		if existingMode == requestedMode {
			// Same mode - deny
			exitWithError(fmt.Sprintf("session already exists in %s mode for %s/%s", existingMode, nodeName, volumeName), nil)
		} else {
			// Different mode - ask user
			fmt.Printf("Session exists in %s mode. Close and reopen in %s mode? (y/N): ", existingMode, requestedMode)
			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))
			if response != "y" && response != "yes" {
				fmt.Println("Aborted.")
				return
			}
			// Delete existing session (waits for pod deletion)
			if err := volume.StopSession(ctx, k8sClient, nodeName, volumeName); err != nil {
				exitWithError("failed to stop existing session", err)
			}
			fmt.Println("Existing session stopped.")
		}
	}

	if sessionRunMode {
		// Run mode
		runGPUSession(ctx, k8sClient, nodeName, volumeName, mounts)
	} else {
		// Edit mode (default)
		runEditSession(ctx, k8sClient, nodeName, volumeName, mounts)
	}
}

func runEditSession(ctx context.Context, k8sClient *client.Client, nodeName, volumeName string, mounts []volume.MountOption) {
	opts := volume.EditOptions{
		NodeName:   nodeName,
		VolumeName: volumeName,
		Mounts:     mounts,
	}

	fmt.Printf("Creating edit session for %s/%s...\n", nodeName, volumeName)

	result, err := volume.Edit(ctx, k8sClient, opts)
	if err != nil {
		exitWithError("", err)
	}

	podName := result.PodName

	if result.Existing {
		fmt.Println("Session already exists, reusing...")
	}

	fmt.Printf("Edit session created: %s/%s\n", nodeName, volumeName)
	fmt.Printf("Use 'sgs attach %s/%s' to attach to the session\n", nodeName, volumeName)
	fmt.Printf("Use 'sgs delete session %s/%s' to delete\n", nodeName, volumeName)

	if sessionAttach {
		// Wait for pod to be ready and attach
		fmt.Println("Waiting for pod to be ready...")
		if err := volume.WaitForPodReady(ctx, k8sClient, podName, 5*time.Minute); err != nil {
			exitWithError("pod failed to become ready", err)
		}

		fmt.Println("Attaching to session...")
		if err := volume.Attach(ctx, k8sClient, podName, os.Stdin, os.Stdout, os.Stderr); err != nil {
			exitWithError("failed to attach to pod", err)
		}
	}
}

func runGPUSession(ctx context.Context, k8sClient *client.Client, nodeName, volumeName string, mounts []volume.MountOption) {
	opts := volume.RunOptions{
		NodeName:   nodeName,
		VolumeName: volumeName,
		GPUs:       sessionGPUNum,
		GPUMem:     sessionGPUMem,
		Command:    sessionCmd,
		Mounts:     mounts,
		PinCPU:     sessionPinCPU,
		PinMem:     sessionPinMem,
	}

	fmt.Printf("Creating run session with %d GPU(s) and %d MiB GPU memory on %s/%s...\n", sessionGPUNum, sessionGPUMem, nodeName, volumeName)

	result, err := volume.Run(ctx, k8sClient, opts)
	if err != nil {
		exitWithError("", err)
	}

	podName := result.PodName

	fmt.Printf("Run session created: %s/%s\n", nodeName, volumeName)

	if len(sessionCmd) > 0 {
		fmt.Printf("Use 'sgs logs %s/%s' to view output\n", nodeName, volumeName)
	} else {
		fmt.Printf("Use 'sgs attach %s/%s' to attach to the session\n", nodeName, volumeName)
	}
	fmt.Printf("Use 'sgs delete session %s/%s' to delete\n", nodeName, volumeName)

	if sessionAttach {
		// Wait for pod to be ready and attach
		fmt.Println("Waiting for pod to be ready...")
		if err := volume.WaitForPodReady(ctx, k8sClient, podName, 5*time.Minute); err != nil {
			exitWithError("pod failed to become ready", err)
		}

		fmt.Println("Attaching to session...")
		if err := volume.Attach(ctx, k8sClient, podName, os.Stdin, os.Stdout, os.Stderr); err != nil {
			exitWithError("failed to attach to pod", err)
		}
	}
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
