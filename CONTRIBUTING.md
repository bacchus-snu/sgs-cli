# Contributing to SGS CLI

Thank you for your interest in contributing to SGS CLI! This document provides guidelines and instructions for contributing.

## Development Setup

### Prerequisites

- Go 1.25 or higher
- Access to a Kubernetes cluster (for testing)
- Git

### Clone and Build

```bash
git clone https://github.com/bacchus-snu/sgs-cli.git
cd sgs-cli
go build -o bin/sgs ./cmd/sgs
```

### Running Tests

```bash
go test ./...
```

### Linting

```bash
# Install golangci-lint if not already installed
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run linter
golangci-lint run
```

## Project Structure

```text
sgs-cli/
├── cmd/sgs/              # Application entry point
├── internal/
│   ├── cleanup/          # Interrupt handling and cleanup
│   ├── client/           # Kubernetes client, config, and updates
│   ├── cmd/              # CLI commands (Cobra)
│   ├── node/             # Node operations
│   ├── session/          # Session operations
│   ├── sgs/              # Shared constants and version
│   ├── user/             # User identity from OIDC
│   ├── volume/           # Volume and session management
│   └── workspace/        # Workspace operations
└── .github/workflows/    # CI/CD workflows
```

## Code Style

- Follow standard Go conventions and idioms
- Use `gofmt` for formatting
- Keep functions focused and small
- Add comments for exported functions and types
- Error messages should be lowercase and not end with punctuation

### Naming Conventions

- Use camelCase for unexported identifiers
- Use PascalCase for exported identifiers
- Keep names concise but descriptive

## Making Changes

### Branch Naming

- `feature/description` - New features
- `fix/description` - Bug fixes
- `docs/description` - Documentation updates
- `refactor/description` - Code refactoring

### Commit Messages

Write clear, concise commit messages:

```text
feat: add volume copy progress indicator

- Show bytes transferred during copy
- Display estimated time remaining
- Support quiet mode with --quiet flag
```

Prefixes:

- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation
- `refactor:` - Code refactoring
- `test:` - Adding tests
- `chore:` - Maintenance tasks

### Pull Requests

1. Create a feature branch from `main`
2. Make your changes with clear commits
3. Ensure tests pass and code is formatted
4. Submit a PR with a clear description
5. Address review feedback

## Adding New Commands

1. Create a new file in `internal/cmd/` (e.g., `mycommand.go`)
2. Define the cobra command
3. Register it in `init()` or add to parent command
4. Add tests if applicable

Example:

```go
package cmd

import (
    "fmt"
    "github.com/spf13/cobra"
)

var myCmd = &cobra.Command{
    Use:   "mycommand",
    Short: "Short description",
    Long:  `Longer description with examples.`,
    Run:   runMyCommand,
}

func init() {
    rootCmd.AddCommand(myCmd)
}

func runMyCommand(cmd *cobra.Command, args []string) {
    // Implementation
}
```

## Modifying Constants

Constants are defined in `internal/sgs/constants.go`. These are hardcoded values used throughout the CLI:

- Label keys for Kubernetes resources
- Annotation keys
- Default values (image, storage size)
- Resource limits

If you need to change these, ensure they match the server-side configuration.

## Version and Releases

### Version Format

We use semantic versioning: `vMAJOR.MINOR.PATCH`

- MAJOR: Breaking changes
- MINOR: New features (backward compatible)
- PATCH: Bug fixes

### Creating a Release

1. Update version-related documentation if needed
1. Create and push a tag:

   ```bash
   git tag v1.2.3
   git push origin v1.2.3
   ```

1. GitHub Actions will automatically:
   - Build binaries for all platforms
   - Create a GitHub release
   - Attach binaries to the release

### Building with Version

For local builds with a specific version:

```bash
go build -ldflags "-X github.com/bacchus-snu/sgs-cli/internal/sgs.Version=v1.2.3" -o bin/sgs ./cmd/sgs
```

## Testing

### Manual Testing

Before submitting a PR, test your changes:

```bash
# Build
go build -o bin/sgs ./cmd/sgs

# Test basic commands
bin/sgs version
bin/sgs --help
bin/sgs get --help
```

### Integration Testing

If you have access to a test cluster:

```bash
bin/sgs fetch
bin/sgs set workspace test-workspace
bin/sgs get nodes
bin/sgs get volumes
```

## Getting Help

- Open an issue for bugs or feature requests
- Check existing issues before creating new ones
- Join discussions in pull requests

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
