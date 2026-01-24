// Package sgs provides shared constants and types for the SGS CLI.
package sgs

// Version is set via -ldflags during build.
// Example: go build -ldflags "-X github.com/bacchus-snu/sgs-cli/internal/sgs.Version=v1.0.0" ./cmd/sgs
var Version = "dev"
