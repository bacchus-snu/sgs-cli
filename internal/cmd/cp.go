package cmd

import (
	"context"
	"fmt"

	"github.com/bacchus-snu/sgs-cli/internal/cleanup"
	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/bacchus-snu/sgs-cli/internal/volume"
	"github.com/spf13/cobra"
)

var cpCmd = &cobra.Command{
	Use:   "cp <source-node>/<source-volume> <dest-node>/<dest-volume>",
	Short: "Copy a volume to another location",
	Long: `Copy the contents of a source volume to a new destination volume.

The destination volume will be created automatically with the same size and type
as the source volume. If the source is an OS volume, the destination will also
be marked as an OS volume.

The source volume must not have an active session. The destination volume must
not already exist.

For same-node copies, this uses a single temporary pod that mounts both volumes.
For cross-node copies, this streams data via tar between two temporary pods.

Examples:
  # Copy a volume on the same node
  sgs cp ferrari/my-volume ferrari/my-volume-backup

  # Copy a volume to a different node
  sgs cp ferrari/my-volume porsche/my-volume

  # Copy an OS volume to create a fresh copy
  sgs cp bentley/os-volume ford/os-volume`,
	Args: cobra.ExactArgs(2),
	Run:  runCp,
}

func init() {
	rootCmd.AddCommand(cpCmd)
}

func runCp(cmd *cobra.Command, args []string) {
	// Use InterruptibleContext which captures SIGINT/SIGTERM and prevents
	// the default "kill process" behavior. When interrupted, context is
	// cancelled, operations fail, functions return, defers run cleanup.
	ctx, cancel := cleanup.InterruptibleContext(context.Background())
	defer cancel()

	// Parse source
	srcNode, srcVolume, err := volume.ParseVolumePath(args[0])
	if err != nil {
		exitWithError(fmt.Sprintf("invalid source: %s", args[0]), err)
	}

	// Parse destination
	dstNode, dstVolume, err := volume.ParseVolumePath(args[1])
	if err != nil {
		exitWithError(fmt.Sprintf("invalid destination: %s", args[1]), err)
	}

	k8sClient, err := client.New()
	if err != nil {
		exitWithError("failed to create client", err)
	}

	fmt.Printf("Copying %s/%s to %s/%s...\n", srcNode, srcVolume, dstNode, dstVolume)

	err = volume.Copy(ctx, k8sClient, volume.CopyOptions{
		SrcNode:   srcNode,
		SrcVolume: srcVolume,
		DstNode:   dstNode,
		DstVolume: dstVolume,
	})
	if err != nil {
		// If context was cancelled (interrupt), signal handler already cleaned up and will exit
		// Just return to let it handle things
		if ctx.Err() != nil {
			return
		}
		exitWithError("", err)
	}
}
