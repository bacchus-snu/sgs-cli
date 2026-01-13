package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
	Use:   "session <node>/<volume>[/<number>]",
	Short: "Delete a session",
	Long: `Delete a session.

Without session number: Deletes the edit session (session 0).
With session number: Deletes the specified session.

Examples:
  # Delete the edit session (session 0)
  sgs delete session ferrari/os-volume

  # Delete a specific run session
  sgs delete session ferrari/os-volume/1`,
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
		exitWithError("failed to delete volume", err)
	}

	fmt.Printf("Volume %s/%s deleted successfully\n", nodeName, volumeName)
}

func runDeleteSession(cmd *cobra.Command, args []string) {
	sessionPath := args[0]

	// Parse path: <node>/<volume> or <node>/<volume>/<number>
	parts := strings.Split(sessionPath, "/")
	if len(parts) < 2 || len(parts) > 3 {
		exitWithError("invalid session path format, expected: <node>/<volume> or <node>/<volume>/<number>", nil)
	}

	nodeName := parts[0]
	volumeName := parts[1]
	sessionNumber := 0 // Default to edit session

	if len(parts) == 3 {
		var err error
		sessionNumber, err = strconv.Atoi(parts[2])
		if err != nil {
			exitWithError("invalid session number", err)
		}
	}

	if nodeName == "" || volumeName == "" {
		exitWithError("invalid session path format, expected: <node>/<volume> or <node>/<volume>/<number>", nil)
	}

	ctx := context.Background()

	k8sClient, err := client.New()
	if err != nil {
		exitWithError("failed to create client", err)
	}

	sessionType := "edit"
	if sessionNumber > 0 {
		sessionType = "run"
	}

	fmt.Printf("Deleting %s session %d for %s/%s...\n", sessionType, sessionNumber, nodeName, volumeName)

	if err := volume.StopSession(ctx, k8sClient, nodeName, volumeName, sessionNumber); err != nil {
		exitWithError("failed to delete session", err)
	}

	fmt.Printf("Session %s/%s/%d deleted successfully\n", nodeName, volumeName, sessionNumber)
}
