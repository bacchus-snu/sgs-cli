# SGS - SNUCSE GPU Service CLI

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A command line interface for SNUCSE GPU Service. It provides a VM-like experience for GPU computing on Kubernetes, abstracting away Kubernetes complexity.

## Features

- **VM-like Experience**: Create persistent volumes that feel like virtual machines
- **Simple Interface**: Only three concepts - nodes, volumes, and sessions
- **Workspace Management**: Namespace-based workspace isolation and resource quota management, shared with multiple users

## Project Structure

```text
sgs-cli/
├── cmd/
│   └── sgs/              # Application entry point
│       └── main.go
├── internal/             # Private application code
│   ├── cleanup/          # Interrupt handling and cleanup registry
│   ├── client/           # Kubernetes client with retry logic
│   ├── cmd/              # CLI commands (Cobra)
│   ├── node/             # Node operations
│   ├── session/          # Session operations
│   ├── sgs/              # Shared configuration and constants
│   ├── user/             # User identity from OIDC tokens
│   ├── volume/           # Volume and session management
│   └── workspace/        # Workspace operations
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## Configuration

SGS CLI downloads configuration files to `~/.sgs/` on first run or when `sgs fetch` is executed:

- `~/.sgs/config.yaml` - Kubernetes kubeconfig for cluster access
- `~/.sgs/metadata.yaml` - CLI metadata (last fetch timestamp)
- `~/.sgs/cache/` - Token cache for OIDC authentication

The configuration is automatically refreshed if more than 24 hours have passed since the last fetch.

## Prerequisites

- Go 1.25 or higher
- Access to SNUCSE GPU Service

## Installation

### Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/bacchus-snu/sgs-cli/main/scripts/install.sh | sh
```

Or with wget:

```bash
wget -qO- https://raw.githubusercontent.com/bacchus-snu/sgs-cli/main/scripts/install.sh | sh
```

The installer will:

- Auto-detect your OS and architecture
- Show available versions to choose from
- Let you select installation path (global `/usr/local/bin` or local `~/.local/bin`)

### Manual Download

Download binaries directly from [GitHub Releases](https://github.com/bacchus-snu/sgs-cli/releases).

### From Source

```bash
git clone https://github.com/bacchus-snu/sgs-cli.git
cd sgs-cli
make build && make install
```

### Auto-Update

The CLI automatically runs `sgs fetch` when any command is executed and the last fetch was more than 24 hours ago. This checks for new versions and offers to update.

## Build

```bash
# Build the binary
go build ./cmd/sgs

# Or use make
make build

# Build for multiple platforms
make build-all

# Clean build artifacts
make clean
```

## Usage

### Initial Setup

```bash
# Download cluster configuration (also checks for CLI updates)
sgs fetch

# Set your workspace
sgs set workspace <workspace-name>

# Check CLI version
sgs version
```

### List Resources

```bash
# List available nodes
sgs get nodes

# List your volumes
sgs get volumes

# List running sessions
sgs get sessions

# List accessible workspaces
sgs get workspaces
```

### Volume Management

```bash
# Create an OS volume (with default container image)
sgs create volume ferrari/os-volume --image

# Create an OS volume with custom image
sgs create volume ferrari/os-volume --image pytorch/pytorch:2.0.0-cuda11.7-cudnn8-devel

# Create a data volume (storage only)
sgs create volume ferrari/data-vol --size 100Gi

# Copy a volume (same or different node)
sgs cp ferrari/os-volume porsche/os-volume

# Delete a volume
sgs delete volume ferrari/os-volume
```

### Session Management

Sessions run on OS volumes. Two types exist:

- **Edit session (0)**: Interactive shell, no GPU, limited resources
- **Run session (1+)**: GPU workloads with specified command, may be shutdown under low GPU utilization

```bash
# Start an edit session (interactive shell)
sgs create session ferrari/os-volume

# Start an edit session with mounted data volume
sgs create session ferrari/os-volume --mount ferrari/data-vol:/data

# Start a run session with GPU (auto-assign session number)
sgs create session ferrari/os-volume --gpu 2 --command "python train.py"

# Start a run session with specific session number
sgs create session ferrari/os-volume/3 --gpu 1 --command "python train.py"

# View session logs (session number optional, defaults to 0)
sgs logs ferrari/os-volume
sgs logs ferrari/os-volume/1

# Delete the edit session (session 0)
sgs delete session ferrari/os-volume

# Delete a specific run session
sgs delete session ferrari/os-volume/1
```

## Concepts

### Nodes

Worker nodes in the cluster where volumes can be created.

### Volumes

Persistent storage that behaves like VM disks. Two types:

- **OS Volume**: Created with `--image`, can run sessions
- **Data Volume**: Storage only, can be mounted into sessions

### Sessions

Running pods on OS volumes. Named as `<node>/<volume>/<number>`:

- Session 0: Edit mode (interactive shell, no GPU)
- Session 1+: Run mode (GPU workloads with command)

### Workspaces

Isolated namespaces for organizing volumes. Set with `sgs set workspace <name>`.

## Development

```bash
# Run tests
make test

# Run linter
make lint

# Format code
make fmt

# Run all checks
make check
```

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- [bacchus-snu](https://github.com/bacchus-snu) - SNUCSE Bacchus Team
