package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/bacchus-snu/sgs-cli/internal/cleanup"
	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/bacchus-snu/sgs-cli/internal/volume"
	"github.com/spf13/cobra"
)

var cpForce bool

var cpCmd = &cobra.Command{
	Use:   "cp <source> <destination>",
	Short: "Copy volumes or files/directories between volumes",
	Long: `Copy volumes or files/directories between volumes.

Volume copy (entire volume):
  sgs cp <node>/<volume> <node>/<volume>
  - Copies entire volume contents
  - Destination volume is created automatically
  - Requires confirmation (use --force to skip)

File/directory copy (specific paths):
  sgs cp <node>/<volume>:<path> <node>/<volume>:<path>
  - Copies specific files or directories within volumes
  - Both source and destination volumes must exist
  - For OS volumes, paths are relative to the rootfs (handled internally)

Note: Source and destination must BOTH have paths (file/directory copy) or NEITHER have paths (volume copy).

Examples:
  # Copy entire volume (creates destination)
  sgs cp ferrari/os-vol porsche/os-vol-backup

  # Copy entire volume, skip confirmation
  sgs cp --force ferrari/os-vol porsche/os-vol-backup

  # Copy specific directory between data volumes
  sgs cp ferrari/data:/datasets/mnist porsche/data:/datasets/mnist

  # Copy from OS volume to data volume
  sgs cp ferrari/os-vol:/home/user/code ferrari/data:/backup/code

  # Copy between different nodes
  sgs cp ferrari/data:/models porsche/data:/models`,
	Args: cobra.ExactArgs(2),
	Run:  runCp,
}

func init() {
	rootCmd.AddCommand(cpCmd)
	cpCmd.Flags().BoolVarP(&cpForce, "force", "f", false, "Skip confirmation prompt (volume copy only)")
}

func runCp(cmd *cobra.Command, args []string) {
	// Use InterruptibleContext which captures SIGINT/SIGTERM and prevents
	// the default "kill process" behavior. When interrupted, context is
	// cancelled, operations fail, functions return, defers run cleanup.
	ctx, cancel := cleanup.InterruptibleContext(context.Background())
	defer cancel()

	// Parse source and destination using the parser that handles paths
	srcPath, err := volume.ParseCopyPath(args[0])
	if err != nil {
		exitWithError(fmt.Sprintf("invalid source: %s", args[0]), err)
	}

	dstPath, err := volume.ParseCopyPath(args[1])
	if err != nil {
		exitWithError(fmt.Sprintf("invalid destination: %s", args[1]), err)
	}

	// Validate that both have paths or neither has paths
	srcHasPath := srcPath.IsFineGrained()
	dstHasPath := dstPath.IsFineGrained()

	if srcHasPath != dstHasPath {
		exitWithError("invalid copy format: source and destination must both have paths (file/directory copy) or neither have paths (volume copy)", nil)
	}

	k8sClient, err := client.New()
	if err != nil {
		exitWithError("failed to create client", err)
	}

	opts := volume.CopyOptions{
		SrcNode:   srcPath.NodeName,
		SrcVolume: srcPath.VolumeName,
		SrcPath:   srcPath.Path,
		DstNode:   dstPath.NodeName,
		DstVolume: dstPath.VolumeName,
		DstPath:   dstPath.Path,
	}

	if srcHasPath {
		// File/directory copy: both volumes must exist
		fmt.Printf("Copying %s/%s:%s to %s/%s:%s...\n",
			srcPath.NodeName, srcPath.VolumeName, srcPath.Path,
			dstPath.NodeName, dstPath.VolumeName, dstPath.Path)

		err = volume.CopyFiles(ctx, k8sClient, opts)
	} else {
		// Volume copy: destination will be created
		srcVolPath := fmt.Sprintf("%s/%s", srcPath.NodeName, srcPath.VolumeName)
		dstVolPath := fmt.Sprintf("%s/%s", dstPath.NodeName, dstPath.VolumeName)

		// Require confirmation unless --force is set
		if !cpForce {
			fmt.Printf("This will copy volume '%s' to new volume '%s'.\n", srcVolPath, dstVolPath)
			fmt.Printf("Type the destination volume path to confirm: ")

			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				exitWithError("failed to read input", err)
			}

			input = strings.TrimSpace(input)
			if input != dstVolPath {
				fmt.Println("Aborted: confirmation does not match")
				os.Exit(1)
			}
		}

		fmt.Printf("Copying %s to %s...\n", srcVolPath, dstVolPath)
		err = volume.Copy(ctx, k8sClient, opts)
	}

	if err != nil {
		// If context was cancelled (interrupt), signal handler already cleaned up and will exit
		// Just return to let it handle things
		if ctx.Err() != nil {
			return
		}
		exitWithError("", err)
	}
}
