// SGS CLI - Command line interface for SNUCSE GPU Service.
// This is the main entry point for the sgs command.
package main

import (
	"os"

	"github.com/bacchus-snu/sgs-cli/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
