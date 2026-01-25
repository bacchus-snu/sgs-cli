package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/bacchus-snu/sgs-cli/internal/volume"
	"github.com/spf13/cobra"
)

var attachCmd = &cobra.Command{
	Use:     "attach <node>/<volume>",
	Aliases: []string{"at"},
	Short:   "Attach to an existing session (at)",
	Long: `Attach to an existing session (edit or run pod).

This command waits for the session pod to be ready and then attaches to it.

Session path format: <node>/<volume>

Examples:
  # Attach to an existing session
  sgs attach ferrari/os-volume`,
	Args: cobra.ExactArgs(1),
	Run:  runAttach,
}

func init() {
	// No additional flags for attach command
}

func runAttach(cmd *cobra.Command, args []string) {
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
		exitWithError(fmt.Sprintf("no active session found for %s/%s", nodeName, volumeName), nil)
	}

	fmt.Printf("Attaching to %s session %s/%s...\n", mode, nodeName, volumeName)

	// Convert to pod name: <node>-<volume>
	podName := fmt.Sprintf("%s-%s", nodeName, volumeName)

	// Wait for pod to be ready
	fmt.Println("Waiting for pod to be ready...")
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	if err := volume.WaitForPodReady(waitCtx, k8sClient, podName, 10*time.Minute); err != nil {
		exitWithError("failed waiting for pod", err)
	}

	// Attach to the session
	if err := volume.Attach(ctx, k8sClient, podName, os.Stdin, os.Stdout, os.Stderr); err != nil {
		exitWithError("failed to attach to session", err)
	}
}
