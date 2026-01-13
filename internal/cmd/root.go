package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "sgs",
	Short: "SGS - SNUCSE GPU Service CLI",
	Long: `SGS is a command line interface for SNUCSE GPU Service.
It provides a VM-like experience for GPU computing on Kubernetes.

Resource Types:
  cluster   - The GPU cluster (nodes)
  workspace - Your namespace for organizing volumes and sessions
  volume    - Persistent storage (OS volumes and data volumes)
  session   - Running pods (edit and run modes)

Commands:
  fetch              - Download cluster configuration
  set workspace      - Set current workspace
  get                - List resources (nodes, volumes, sessions, workspaces)
  create volume      - Create a new volume
  create session     - Create a session (edit or run)
  delete volume      - Delete a volume
  delete session     - Delete a session
  logs               - View session logs

Session Types:
  Edit session (session 0): Interactive shell, no GPU, limited resources
  Run session (session 1+): GPU workloads with specified command

Examples:
  # Initial setup
  sgs fetch                                 # Download cluster config
  sgs set workspace <name>                  # Set your workspace

  # List resources
  sgs get nodes                             # List available nodes
  sgs get volumes                           # List your volumes
  sgs get sessions                          # List running sessions

  # Create and use volumes
  sgs create volume ferrari/os-volume --image  # Create OS volume
  sgs create session ferrari/os-volume         # Start edit session (shell)
  sgs create session ferrari/os-volume --gpu 2 --command "python train.py"

  # Manage sessions
  sgs logs ferrari/os-volume/1                 # View run session logs
  sgs delete session ferrari/os-volume         # Delete edit session (0)
  sgs delete session ferrari/os-volume/1       # Delete run session 1`,
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
	rootCmd.AddCommand(fetchCmd)
}

// helper function to print errors and exit
func exitWithError(msg string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s: %v\n", msg, err)
	} else {
		fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
	}
	os.Exit(1)
}
