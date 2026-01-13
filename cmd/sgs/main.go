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
