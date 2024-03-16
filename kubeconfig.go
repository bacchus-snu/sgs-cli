package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func DownloadKubeconfig() error {
	kubeconfigURL := "https://raw.githubusercontent.com/bacchus-snu/sgs-cli/main/config.yaml"
	destinationPath := filepath.Join(os.Getenv("HOME"), ".sgs", "config.yaml")

	// Check if the file already exists
	if _, err := os.Stat(destinationPath); os.IsNotExist(err) {
		// Create the directory if it doesn't exist
		err := os.MkdirAll(filepath.Dir(destinationPath), 0755)
		if err != nil {
			return fmt.Errorf("Failed to create directory: %w", err)
		}

		// Download the kubeconfig file
		response, err := http.Get(kubeconfigURL)
		if err != nil {
			return fmt.Errorf("Failed to download kubeconfig file: %w", err)
		}
		defer response.Body.Close()

		// Create the file
		file, err := os.Create(destinationPath)
		if err != nil {
			return fmt.Errorf("Failed to create file: %w", err)
		}
		defer file.Close()

		// Copy the response body to the file
		_, err = io.Copy(file, response.Body)
		if err != nil {
			return fmt.Errorf("Failed to save kubeconfig file: %w", err)
		}

		log.Printf("Kubeconfig file downloaded successfully")
	} else {
		log.Printf("Kubeconfig file already exists")
	}

	return nil
}
