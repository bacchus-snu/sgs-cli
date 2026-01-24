// Package cmd provides the command-line interface for SGS.
// It implements commands for volume, session, workspace, and node management.
package cmd

import (
	"fmt"
	"os"

	"github.com/bacchus-snu/sgs-cli/internal/sgs"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "sgs",
	Short: "SGS - SNUCSE GPU Service CLI",
	Long: `SGS is a command line interface for SNUCSE GPU Service.
It provides a VM-like experience for GPU computing on Kubernetes.

Resource Types:
  workspace - Your namespace for organizing volumes and sessions
  node      - Worker nodes in the GPU cluster
  volume    - Persistent storage (OS volumes and data volumes)
  session   - Running pods (edit and run modes)

Commands:
  fetch              - Download cluster configuration
  set workspace      - Set current workspace
  get                - List resources (nodes, volumes, sessions, workspaces)
  create volume      - Create a new volume
  create session     - Create a session (edit or run)
  cp                 - Copy a volume to another location
  delete volume      - Delete a volume
  delete session     - Delete a session
  logs               - View session logs
  attach             - Attach to an existing session

Session Types:
  Edit session: Interactive shell, no GPU, for code editing (default)
  Run session:  GPU workloads with specified command (--run flag)

Note: Only one session can exist per volume at a time.
To switch from edit to run mode (or vice versa), you'll be prompted to confirm.

Examples:
  # Initial setup
  sgs fetch                                 # Download cluster config
  sgs set workspace <name>                  # Set your workspace

  # List resources
  sgs get workspaces                        # List accessible workspaces
  sgs get nodes                             # List available nodes
  sgs get volumes                           # List your volumes
  sgs get sessions                          # List running sessions

  # Create and use volumes
  sgs create volume ferrari/os-volume --image  # Create OS volume
  sgs cp ferrari/os-volume porsche/os-volume   # Copy volume to another node
  sgs create session ferrari/os-volume         # Start edit session (shell)
  sgs create session ferrari/os-volume --run --gpu-num 2 --gpu-mem 8192 --command "python train.py"

  # Attach to existing session
  sgs attach ferrari/os-volume

  # Manage sessions
  sgs logs ferrari/os-volume                   # View session logs
  sgs delete session ferrari/os-volume         # Delete session`,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("sgs version %s\n", sgs.Version)
	},
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(getCmd)
	rootCmd.AddCommand(describeCmd)
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(deleteCmd)
	rootCmd.AddCommand(setCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(attachCmd)
	rootCmd.AddCommand(fetchCmd)
	rootCmd.AddCommand(versionCmd)
}

// exitWithError prints an error and exits with code 1.
// If err is provided, it prints "Error: <err>" (the error is expected to be user-friendly).
// If msg is also provided, it prints "Error: <msg>: <err>".
func exitWithError(msg string, err error) {
	if err != nil {
		if msg != "" {
			fmt.Fprintf(os.Stderr, "Error: %s: %v\n", msg, err)
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
	}
	os.Exit(1)
}
