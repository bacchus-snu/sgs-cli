package main

import (
	"fmt"
)

func main() {
	// Download the kubeconfig file
	DownloadKubeconfig()

	// Parse the configuration
	behavior, subject, sgsConfig := ParseSGSConfig()

	// Check the configuration
	CheckSGSConfig(behavior, subject, sgsConfig)

	// Get the token
	token := GetToken()

	fmt.Println(token)
}
