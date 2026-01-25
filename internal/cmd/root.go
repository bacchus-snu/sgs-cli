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

Resource Types (aliases):
  all                 All resources
  node (no)           Worker nodes in the GPU cluster
  session (se)        Running pods (edit and run modes)
  volume (vo, vol)    Persistent storage (OS volumes and data volumes)
  workspace (ws)      Your space for volumes, sessions, and resource quotas

Volume Types:
  os    Bootable volume with container image overlay (for sessions)
  data  Storage-only volume for datasets and models

Session Types:
  edit  Interactive shell, no GPU, for code editing (default)
  run   GPU workloads with specified command (--run flag)

Notes:
  - Sessions can only be created from OS volumes (not data volumes)
  - Only one session can be created from an OS volume at a time
  - Both OS and data volumes can be mounted on multiple sessions simultaneously

Examples:
  sgs fetch                              # Download cluster config
  sgs set workspace <name>               # Set your workspace
  sgs get nodes                          # List available nodes (or: sgs get no)
  sgs get volumes                        # List your volumes (or: sgs get vo)
  sgs create volume ferrari/os --image   # Create OS volume
  sgs create session ferrari/os          # Start edit session
  sgs create session ferrari/os --run --gpu-num 2 --command "python train.py"
  sgs attach ferrari/os                  # Attach to session (or: sgs at ferrari/os)
  sgs logs ferrari/os                    # View logs (or: sgs log ferrari/os)
  sgs delete session ferrari/os          # Delete session`,
}

var versionCmd = &cobra.Command{
	Use:     "version",
	Aliases: []string{"ver"},
	Short:   "Print the version number (ver)",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("sgs version %s\n", sgs.Version)
	},
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Disable the default "help" subcommand (use --help flag instead)
	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})

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
