package cmd

import (
	"context"
	"fmt"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/bacchus-snu/sgs-cli/internal/session"
	"github.com/bacchus-snu/sgs-cli/internal/volume"
	"github.com/spf13/cobra"
)

var (
	logsFollow bool
	logsTail   int64
)

var logsCmd = &cobra.Command{
	Use:   "logs <node>/<volume>",
	Short: "Print logs from a session",
	Long: `Print logs from a session (edit or run pod).

Session path format: <node>/<volume>

Examples:
  # Print logs from a session
  sgs logs ferrari/os-volume

  # Follow logs
  sgs logs ferrari/os-volume -f

  # Print last 100 lines
  sgs logs ferrari/os-volume --tail 100`,
	Args: cobra.ExactArgs(1),
	Run:  runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().Int64Var(&logsTail, "tail", -1, "Lines of recent log to show")
}

func runLogs(cmd *cobra.Command, args []string) {
	sessionPath := args[0]

	// Parse path: <node>/<volume>
	nodeName, volumeName, err := volume.ParseVolumePath(sessionPath)
	if err != nil {
		exitWithError("invalid session path format, expected: <node>/<volume>", nil)
	}

	// Convert to pod name: <node>-<volume>
	podName := fmt.Sprintf("%s-%s", nodeName, volumeName)

	ctx := context.Background()

	k8sClient, err := client.New()
	if err != nil {
		exitWithError("failed to create client", err)
	}

	opts := session.LogsOptions{
		Follow: logsFollow,
		Tail:   logsTail,
	}

	logs, err := session.Logs(ctx, k8sClient, podName, opts)
	if err != nil {
		exitWithError("", err)
	}

	fmt.Print(logs)
}
