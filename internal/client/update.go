package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bacchus-snu/sgs-cli/internal/sgs"
)

const (
	githubAPIURL = "https://api.github.com/repos/bacchus-snu/sgs-cli/releases/latest"
)

// Release represents a GitHub release
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset represents a release asset (binary file)
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// UpdateInfo contains information about an available update
type UpdateInfo struct {
	CurrentVersion string
	LatestVersion  string
	DownloadURL    string
	Available      bool
}

// GetLatestRelease fetches the latest release info from GitHub
func GetLatestRelease() (*Release, error) {
	resp, err := http.Get(githubAPIURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch release info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch release info: HTTP %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release info: %w", err)
	}

	return &release, nil
}

// CheckForUpdate checks if a newer version is available
func CheckForUpdate() (*UpdateInfo, error) {
	release, err := GetLatestRelease()
	if err != nil {
		return nil, err
	}

	info := &UpdateInfo{
		CurrentVersion: sgs.Version,
		LatestVersion:  release.TagName,
	}

	// Find the binary asset for current OS/arch
	assetURL := GetBinaryAssetURL(release, runtime.GOOS, runtime.GOARCH)
	if assetURL != "" {
		info.DownloadURL = assetURL
	}

	// Compare versions (strip 'v' prefix for comparison)
	current := strings.TrimPrefix(sgs.Version, "v")
	latest := strings.TrimPrefix(release.TagName, "v")

	// Simple comparison - if different and not "dev", update is available
	if current != latest && current != "dev" {
		info.Available = true
	}

	return info, nil
}

// GetBinaryAssetURL finds the download URL for the binary matching the given OS and architecture
func GetBinaryAssetURL(release *Release, goos, goarch string) string {
	// Binary naming convention: sgs-{os}-{arch} or sgs-{os}-{arch}.exe for Windows
	expectedName := fmt.Sprintf("sgs-%s-%s", goos, goarch)
	if goos == "windows" {
		expectedName += ".exe"
	}

	for _, asset := range release.Assets {
		if asset.Name == expectedName {
			return asset.BrowserDownloadURL
		}
	}

	return ""
}

// DownloadBinary downloads a binary from the given URL to a temporary file
func DownloadBinary(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download binary: HTTP %d", resp.StatusCode)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "sgs-update-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()

	// Copy content
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write binary: %w", err)
	}

	// Make executable
	if err := os.Chmod(tmpFile.Name(), 0755); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to set permissions: %w", err)
	}

	return tmpFile.Name(), nil
}

// UpdateBinary replaces the current binary with the new one
func UpdateBinary(tempPath string) error {
	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// Try direct replacement first
	if err := os.Rename(tempPath, execPath); err == nil {
		return nil
	}

	// If direct rename fails (cross-device), try copy
	if err := copyFile(tempPath, execPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	os.Remove(tempPath)
	return nil
}

// UpdateBinaryWithSudo uses sudo to replace the binary
func UpdateBinaryWithSudo(tempPath string) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// Use sudo to move the file
	cmd := exec.Command("sudo", "mv", tempPath, execPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo mv failed: %w", err)
	}

	// Set permissions
	cmd = exec.Command("sudo", "chmod", "755", execPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo chmod failed: %w", err)
	}

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	// Remove destination first to avoid ETXTBSY on running executables.
	// On Linux, you cannot write to a running executable, but you can delete it
	// (the process keeps running via its inode).
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}

	dest, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer dest.Close()

	_, err = io.Copy(dest, source)
	return err
}

// PromptForUpdate checks for updates and prompts the user to update if available
func PromptForUpdate() {
	info, err := CheckForUpdate()
	if err != nil {
		// Silently ignore update check errors
		return
	}

	if !info.Available {
		return
	}

	if info.DownloadURL == "" {
		fmt.Printf("New version available: %s (current: %s)\n", info.LatestVersion, info.CurrentVersion)
		fmt.Println("No binary available for your platform. Please build from source.")
		return
	}

	fmt.Printf("\nNew version available: %s (current: %s)\n", info.LatestVersion, info.CurrentVersion)
	fmt.Print("Do you want to update? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	if response != "y" && response != "yes" {
		return
	}

	fmt.Println("Downloading update...")
	tempPath, err := DownloadBinary(info.DownloadURL)
	if err != nil {
		fmt.Printf("Failed to download update: %v\n", err)
		return
	}

	fmt.Println("Installing update...")
	if err := UpdateBinary(tempPath); err != nil {
		// Check for actual permission errors (handles wrapped errors)
		if os.IsPermission(err) {
			fmt.Print("Requires elevated permissions. Use sudo? [y/N]: ")
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))

			if response == "y" || response == "yes" {
				if err := UpdateBinaryWithSudo(tempPath); err != nil {
					fmt.Printf("Failed to install update: %v\n", err)
					os.Remove(tempPath)
					return
				}
			} else {
				fmt.Println("Update cancelled.")
				os.Remove(tempPath)
				return
			}
		} else {
			fmt.Printf("Failed to install update: %v\n", err)
			os.Remove(tempPath)
			return
		}
	}

	fmt.Printf("Successfully updated to %s\n", info.LatestVersion)
}
