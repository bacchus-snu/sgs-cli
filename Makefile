# Project metadata
APP_NAME := sgs
MODULE := github.com/bacchus-snu/sgs-cli
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Go parameters
GO := go
GOFMT := gofmt
GOLINT := golangci-lint

# Build parameters
BUILD_DIR := bin
MAIN_PACKAGE := ./cmd/sgs
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)"

# Targets
.PHONY: all build build-all clean test lint fmt check install uninstall help

## all: Build the application (default)
all: build

## build: Build the application for current platform
build:
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME) $(MAIN_PACKAGE)
	@echo "Build complete: $(BUILD_DIR)/$(APP_NAME)"

## build-all: Build for multiple platforms
build-all: clean
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-linux-amd64 $(MAIN_PACKAGE)
	GOOS=linux GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-linux-arm64 $(MAIN_PACKAGE)
	GOOS=darwin GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-darwin-amd64 $(MAIN_PACKAGE)
	GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-darwin-arm64 $(MAIN_PACKAGE)
	GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(APP_NAME)-windows-amd64.exe $(MAIN_PACKAGE)
	@echo "Build complete for all platforms"

## clean: Remove build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@echo "Clean complete"

## test: Run tests
test:
	@echo "Running tests..."
	$(GO) test -v -race -cover ./...

## lint: Run linter
lint:
	@echo "Running linter..."
	$(GOLINT) run ./...

## fmt: Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) -w -s .
	$(GO) mod tidy

## check: Run all checks (fmt, lint, test)
check: fmt lint test

## install: Install the binary to GOPATH/bin
install: build
	@echo "Installing $(APP_NAME)..."
	$(GO) install $(LDFLAGS) $(MAIN_PACKAGE)
	@echo "Install complete"

## uninstall: Remove the binary from GOPATH/bin
uninstall:
	@echo "Uninstalling $(APP_NAME)..."
	@rm -f $(shell go env GOPATH)/bin/$(APP_NAME)
	@echo "Uninstall complete"

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod verify

## tidy: Tidy go modules
tidy:
	@echo "Tidying modules..."
	$(GO) mod tidy

## run: Build and run the application
run: build
	./$(BUILD_DIR)/$(APP_NAME)

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
