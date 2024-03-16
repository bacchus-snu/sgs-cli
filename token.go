package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/int128/kubelogin/pkg/di"
	"github.com/int128/kubelogin/pkg/infrastructure/browser"
	"github.com/int128/kubelogin/pkg/infrastructure/clock"
	"github.com/int128/kubelogin/pkg/infrastructure/logger"
)

type ExecCredentialConfig struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Spec       struct {
		Interactive bool `json:"interactive"`
	} `json:"spec"`
	Status struct {
		ExpirationTimestamp string `json:"expirationTimestamp"`
		Token               string `json:"token"`
	} `json:"status"`
}

func GetToken() (string, error) {
	// Create a pipe
	r, w, err := os.Pipe()
	if err != nil {
		return "", fmt.Errorf("Failed to create pipe: %v", err)
	}

	// Create a Cmd instance
	clockReal := &clock.Real{}
	loggerInterface := logger.New()
	browserBrowser := &browser.Browser{}
	cmdInterface := di.NewCmdForHeadless(clockReal, os.Stdin, w, loggerInterface, browserBrowser)

	// Perform OIDC login
	args := []string{
		"oidc-login",
		"get-token",
		"--oidc-issuer-url=https://id.snucse.org/o",
		"--oidc-client-id=kubernetes-oidc",
		"--oidc-client-secret=kubernetes-oidc",
		"--oidc-use-pkce",
	}
	version := "HEAD"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	code := cmdInterface.Run(ctx, args, version)
	if code != 0 {
		return "", fmt.Errorf("Failed to get token. Exit code: %d", code)
	}
	defer w.Close()
	defer cancel()

	// Read the token from the pipe
	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("Failed to read token: %v", err)
	}
	defer r.Close()

	// Parse the result in JSON format
	var result ExecCredentialConfig
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("Failed to parse token: %v", err)
	}

	return result.Status.Token, nil
}
