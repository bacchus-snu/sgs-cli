// Package user provides functionality for extracting user identity
// from OIDC tokens in kubeconfig. It parses JWT tokens to retrieve
// user information such as username and group memberships.
package user

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UserInfo represents the user information from OIDC token
type UserInfo struct {
	Sub      string   `json:"sub"`
	Username string   `json:"username"`
	Groups   []string `json:"groups"`
	Exp      int64    `json:"exp"`
	Iat      int64    `json:"iat"`
}

// tokenCache represents the cached token file format
type tokenCache struct {
	IDToken string `json:"id_token"`
}

// GetCurrentUser returns the current user info from the cached OIDC token
func GetCurrentUser() (*UserInfo, error) {
	cacheDir := filepath.Join(os.Getenv("HOME"), ".sgs", "cache")

	// Find the token cache file (excluding .lock files)
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache directory: %w", err)
	}

	var tokenFile string
	for _, entry := range entries {
		if !entry.IsDir() && !strings.HasSuffix(entry.Name(), ".lock") {
			tokenFile = filepath.Join(cacheDir, entry.Name())
			break
		}
	}

	if tokenFile == "" {
		return nil, fmt.Errorf("no token cache found. Please run 'sgs fetch' first")
	}

	// Read the token cache file
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read token cache: %w", err)
	}

	var cache tokenCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to parse token cache: %w", err)
	}

	// Parse the JWT token (format: header.payload.signature)
	parts := strings.Split(cache.IDToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT token format")
	}

	// Decode the payload (base64url encoded)
	payload := parts[1]
	// Add padding if needed
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		// Try standard base64 if URL encoding fails
		decoded, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
		}
	}

	var userInfo UserInfo
	if err := json.Unmarshal(decoded, &userInfo); err != nil {
		return nil, fmt.Errorf("failed to parse user info: %w", err)
	}

	return &userInfo, nil
}

// HasGroup checks if the user belongs to a specific group
func (u *UserInfo) HasGroup(group string) bool {
	for _, g := range u.Groups {
		if g == group {
			return true
		}
	}
	return false
}
