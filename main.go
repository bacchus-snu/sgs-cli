package main

import (
	"log"
)

func main() {
	// Download the kubeconfig file
	err := DownloadKubeconfig()
	if err != nil {
		log.Fatalf("%v", err)
	}

	// Parse the configuration
	behavior, subject, sgsConfig := ParseSGSConfig()

	// Check the configuration
	CheckSGSConfig(behavior, subject, sgsConfig)

	// Get the token
	token, err := GetToken()
	if err != nil {
		log.Fatalf("%v", err)
	}

	// Send the request with token to the server via kubectl
	SendRequest(behavior, subject, sgsConfig, token)
}
