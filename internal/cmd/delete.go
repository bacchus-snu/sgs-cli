package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/bacchus-snu/sgs-cli/internal/volume"
	"github.com/spf13/cobra"
)

var deleteForce bool

var deleteCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"del"},
	Short:   "Delete a resource (del)",
}

var deleteVolumeCmd = &cobra.Command{
	Use:     "volume <node>/<volume>",
	Aliases: []string{"volumes", "vo", "vol"},
	Short:   "Delete a volume (vo, vol)",
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
	Use:     "session <node>/<volume>",
	Aliases: []string{"sessions", "se"},
	Short:   "Delete a session (se)",
	Long: `Delete a session.

Examples:
  # Delete the session
  sgs delete session ferrari/os-volume`,
	Args: cobra.ExactArgs(1),
	Run:  runDeleteSession,
}

func init() {
	deleteVolumeCmd.Flags().BoolVarP(&deleteForce, "force", "f", false, "Skip confirmation prompt")
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

	// Require confirmation unless --force is set
	if !deleteForce {
		fmt.Printf("WARNING: This will permanently delete volume '%s/%s' and all its data!\n", nodeName, volumeName)
		fmt.Printf("Type the volume name to confirm: ")

		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			exitWithError("failed to read input", err)
		}

		input = strings.TrimSpace(input)
		if input != volumePath {
			fmt.Println("Aborted: confirmation does not match")
			os.Exit(1)
		}
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

	fmt.Printf("Deleting session for %s/%s...\n", nodeName, volumeName)

	// StopSession handles pods in any state (Running, Pending, Failed, Succeeded)
	// and returns an error if the pod doesn't exist
	if err := volume.StopSession(ctx, k8sClient, nodeName, volumeName); err != nil {
		exitWithError("", err)
	}

	fmt.Printf("Session %s/%s deleted successfully\n", nodeName, volumeName)
}
