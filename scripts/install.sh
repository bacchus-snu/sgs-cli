#!/bin/sh
set -e

# Colors (only if terminal supports it)
if [ -t 1 ]; then
  RED='\033[0;31m'
  GREEN='\033[0;32m'
  YELLOW='\033[1;33m'
  BLUE='\033[0;34m'
  NC='\033[0m'
else
  RED='' GREEN='' YELLOW='' BLUE='' NC=''
fi

REPO="bacchus-snu/sgs-cli"
BINARY_NAME="sgs"

# Detect OS and architecture
detect_platform() {
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)

  case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) printf "${RED}Unsupported architecture: %s${NC}\n" "$ARCH"; exit 1 ;;
  esac

  case "$OS" in
    linux) OS="linux" ;;
    darwin) OS="darwin" ;;
    mingw*|msys*|cygwin*) OS="windows" ;;
    *) printf "${RED}Unsupported OS: %s${NC}\n" "$OS"; exit 1 ;;
  esac
}

# Fetch available versions from GitHub
fetch_versions() {
  RELEASES_URL="https://api.github.com/repos/$REPO/releases"
  if command -v curl >/dev/null 2>&1; then
    RELEASES=$(curl -s "$RELEASES_URL" | grep '"tag_name"' | head -10 | cut -d'"' -f4)
  elif command -v wget >/dev/null 2>&1; then
    RELEASES=$(wget -qO- "$RELEASES_URL" | grep '"tag_name"' | head -10 | cut -d'"' -f4)
  else
    printf "${RED}Error: curl or wget required${NC}\n"
    exit 1
  fi
}

# Interactive version selection
select_version() {
  printf "${BLUE}Available versions:${NC}\n"
  printf "  1) latest\n"
  i=2
  for v in $RELEASES; do
    printf "  %d) %s\n" "$i" "$v"
    i=$((i + 1))
  done

  printf "\nSelect version [1]: "
  read -r choice
  choice=${choice:-1}

  if [ "$choice" = "1" ]; then
    VERSION="latest"
  else
    VERSION=$(echo "$RELEASES" | sed -n "$((choice - 1))p")
  fi
}

# Select installation path
select_install_path() {
  printf "\n${BLUE}Installation options:${NC}\n"
  printf "  1) /usr/local/bin (requires sudo)\n"
  printf "  2) ~/.local/bin (no sudo, add to PATH if needed)\n"

  printf "\nSelect installation path [1]: "
  read -r choice
  choice=${choice:-1}

  if [ "$choice" = "1" ]; then
    INSTALL_DIR="/usr/local/bin"
    NEED_SUDO=true
  else
    INSTALL_DIR="$HOME/.local/bin"
    NEED_SUDO=false
    mkdir -p "$INSTALL_DIR"
  fi
}

# Download and install
install_binary() {
  if [ "$VERSION" = "latest" ]; then
    DOWNLOAD_URL="https://github.com/$REPO/releases/latest/download/sgs-$OS-$ARCH"
  else
    DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/sgs-$OS-$ARCH"
  fi

  # Add .exe for Windows
  if [ "$OS" = "windows" ]; then
    DOWNLOAD_URL="${DOWNLOAD_URL}.exe"
    BINARY_NAME="sgs.exe"
  fi

  printf "\n${YELLOW}Downloading %s...${NC}\n" "$BINARY_NAME"

  TMP_FILE=$(mktemp)
  if command -v curl >/dev/null 2>&1; then
    if ! curl -fSL "$DOWNLOAD_URL" -o "$TMP_FILE"; then
      printf "${RED}Download failed. Check your network or the version.${NC}\n"
      rm -f "$TMP_FILE"
      exit 1
    fi
  else
    if ! wget -q "$DOWNLOAD_URL" -O "$TMP_FILE"; then
      printf "${RED}Download failed. Check your network or the version.${NC}\n"
      rm -f "$TMP_FILE"
      exit 1
    fi
  fi

  chmod +x "$TMP_FILE"

  if [ "$NEED_SUDO" = true ]; then
    printf "${YELLOW}Installing to %s (requires sudo)...${NC}\n" "$INSTALL_DIR"
    sudo mv "$TMP_FILE" "$INSTALL_DIR/$BINARY_NAME"
  else
    mv "$TMP_FILE" "$INSTALL_DIR/$BINARY_NAME"
  fi

  printf "\n${GREEN}SGS CLI installed successfully!${NC}\n"
  printf "  Location: %s/%s\n" "$INSTALL_DIR" "$BINARY_NAME"

  # Check if in PATH
  if ! command -v sgs >/dev/null 2>&1; then
    printf "\n${YELLOW}Note: Add %s to your PATH:${NC}\n" "$INSTALL_DIR"
    printf "  export PATH=\"\$PATH:%s\"\n" "$INSTALL_DIR"
  fi

  printf "\nRun 'sgs fetch' to get started.\n"
}

# Main
main() {
  printf "${BLUE}======================================${NC}\n"
  printf "${BLUE}       SGS CLI Installer              ${NC}\n"
  printf "${BLUE}======================================${NC}\n\n"

  detect_platform
  printf "Detected: %s-%s\n\n" "$OS" "$ARCH"

  fetch_versions

  # Check if any releases exist
  if [ -z "$RELEASES" ]; then
    printf "${YELLOW}No releases found. Installing latest...${NC}\n"
    VERSION="latest"
  else
    select_version
  fi

  select_install_path
  install_binary
}

main
