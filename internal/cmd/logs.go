package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/bacchus-snu/sgs-cli/internal/client"
	"github.com/bacchus-snu/sgs-cli/internal/session"
	"github.com/spf13/cobra"
)

var (
	logsFollow bool
	logsTail   int64
)

var logsCmd = &cobra.Command{
	Use:   "logs <node>/<volume>[/<number>]",
	Short: "Print logs from a session",
	Long: `Print logs from a session (edit or run pod).

Session path format: <node>/<volume>[/<number>]
  - Session number is optional, defaults to 0 (edit session)
  - Session 0 is the edit session
  - Sessions 1+ are run sessions

Examples:
  # Print logs from an edit session (session 0)
  sgs logs ferrari/os-volume

  # Print logs from a run session
  sgs logs ferrari/os-volume/1

  # Follow logs
  sgs logs ferrari/os-volume/1 -f

  # Print last 100 lines
  sgs logs ferrari/os-volume/1 --tail 100`,
	Args: cobra.ExactArgs(1),
	Run:  runLogs,
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output")
	logsCmd.Flags().Int64Var(&logsTail, "tail", -1, "Lines of recent log to show")
}

func runLogs(cmd *cobra.Command, args []string) {
	sessionPath := args[0]

	// Parse path: <node>/<volume>[/<number>]
	parts := strings.Split(sessionPath, "/")
	if len(parts) < 2 || len(parts) > 3 {
		exitWithError("invalid session path format, expected: <node>/<volume>[/<number>]", nil)
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
		exitWithError("invalid session path format, expected: <node>/<volume>[/<number>]", nil)
	}

	// Convert to pod name: <node>-<volume>-<number>
	podName := fmt.Sprintf("%s-%s-%d", nodeName, volumeName, sessionNumber)

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
		exitWithError("failed to get logs", err)
	}

	fmt.Print(logs)
}
