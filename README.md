# SGS - SNUCSE GPU Service CLI

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A command line interface for SNUCSE GPU Service. It provides a VM-like experience for GPU computing on Kubernetes, abstracting away Kubernetes complexity.

## Features

- **VM-like Experience**: Create persistent volumes that feel like virtual machines
- **Simple Interface**: Only two concepts - nodes and volumes
- **GPU Support**: Integrated with HAMI scheduler for GPU resource management
- **Workspace Management**: Namespace-based workspace isolation

## Project Structure

```
sgs-cli/
├── cmd/
│   └── sgs/              # Application entry point
│       └── main.go
├── internal/             # Private application code
│   ├── client/           # Kubernetes client
│   ├── cmd/              # CLI commands
│   ├── node/             # Node operations
│   ├── session/          # Session operations
│   ├── user/             # User operations
│   ├── volume/           # Volume operations
│   └── workspace/        # Workspace operations
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## Prerequisites

- Go 1.25 or higher
- Access to SNUCSE GPU Service

## Installation

### From Source

```bash
git clone https://github.com/bacchus-snu/sgs-cli.git
cd sgs-cli
make build
```

### Install to PATH

```bash
make install
```

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
# Download cluster configuration
sgs fetch

# Set your workspace
sgs set workspace <workspace-name>
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
# Create an OS volume (with container image)
sgs create volume ferrari/os-volume --image

# Create an OS volume with custom image
sgs create volume ferrari/os-volume --image pytorch/pytorch:2.0.0-cuda11.7-cudnn8-devel

# Create a data volume (storage only)
sgs create volume ferrari/data-vol --size 100Gi

# Delete a volume
sgs delete volume ferrari/os-volume
```

### Session Management

Sessions run on OS volumes. Two types exist:

- **Edit session (0)**: Interactive shell, no GPU, limited resources
- **Run session (1+)**: GPU workloads with specified command

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
