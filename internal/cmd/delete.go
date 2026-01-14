package cmd

import (
	"context"
	"fmt"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/bacchus-snu/sgs-cli/internal/volume"
	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a resource",
}

var deleteVolumeCmd = &cobra.Command{
	Use:   "volume <node>/<volume>",
	Short: "Delete a volume",
	Long: `Delete a persistent volume.

This will delete both the Pod and the PersistentVolumeClaim.
WARNING: All data in the volume will be lost!

Examples:
  # Delete a volume
  sgs delete volume ferrari/my-workspace`,
	Args: cobra.ExactArgs(1),
	Run:  runDeleteVolume,
}

var deleteSessionCmd = &cobra.Command{
	Use:   "session <node>/<volume>",
	Short: "Delete a session",
	Long: `Delete a session.

Examples:
  # Delete the session
  sgs delete session ferrari/os-volume`,
	Args: cobra.ExactArgs(1),
	Run:  runDeleteSession,
}

func init() {
	deleteCmd.AddCommand(deleteVolumeCmd)
	deleteCmd.AddCommand(deleteSessionCmd)
}

func runDeleteVolume(cmd *cobra.Command, args []string) {
	volumePath := args[0]

	// Parse node/volume path
	nodeName, volumeName, err := volume.ParseVolumePath(volumePath)
	if err != nil {
		exitWithError("invalid volume path", err)
	}

	ctx := context.Background()

	k8sClient, err := client.New()
	if err != nil {
		exitWithError("failed to create client", err)
	}

	fmt.Printf("Deleting volume %s/%s...\n", nodeName, volumeName)

	if err := volume.Delete(ctx, k8sClient, nodeName, volumeName); err != nil {
		exitWithError("", err)
	}

	fmt.Printf("Volume %s/%s deleted successfully\n", nodeName, volumeName)
}

func runDeleteSession(cmd *cobra.Command, args []string) {
	sessionPath := args[0]

	// Parse path: <node>/<volume>
	nodeName, volumeName, err := volume.ParseVolumePath(sessionPath)
	if err != nil {
		exitWithError("invalid session path format, expected: <node>/<volume>", nil)
	}

	ctx := context.Background()

	k8sClient, err := client.New()
	if err != nil {
		exitWithError("failed to create client", err)
	}

	// Check if session exists
	mode, err := volume.GetSessionMode(ctx, k8sClient, nodeName, volumeName)
	if err != nil {
		exitWithError("", err)
	}

	if mode == "" {
		exitWithError(fmt.Sprintf("no session found for %s/%s", nodeName, volumeName), nil)
	}

	fmt.Printf("Deleting %s session for %s/%s...\n", mode, nodeName, volumeName)

	if err := volume.StopSession(ctx, k8sClient, nodeName, volumeName); err != nil {
		exitWithError("", err)
	}

	fmt.Printf("Session %s/%s deleted successfully\n", nodeName, volumeName)
}
