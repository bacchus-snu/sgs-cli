package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"

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

func GetToken() string {
	// Create a pipe
	r, w, err := os.Pipe()
	if err != nil {
		log.Fatalf("Failed to create pipe: %v", err)
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
	code := cmdInterface.Run(context.Background(), args, version)
	if code != 0 {
		log.Fatalf("Failed to get token: %d", code)
	}
	w.Close()

	// Read the token from the pipe
	data, err := io.ReadAll(r)
	if err != nil {
		log.Fatalf("Failed to read token: %v", err)
	}
	r.Close()

	// Parse the result in JSON format
	var result ExecCredentialConfig
	if err := json.Unmarshal(data, &result); err != nil {
		log.Fatalf("Failed to parse token: %v", err)
	}

	return result.Status.Token
}
