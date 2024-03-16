package main

func main() {
	// Download the kubeconfig file
	DownloadKubeconfig()

	// Parse the configuration
	behavior, subject, sgsConfig := ParseSGSConfig()

	// Check the configuration
	CheckSGSConfig(behavior, subject, sgsConfig)

	// Get the token
	token := GetToken()

	// Get the request
	request := GetRequest(behavior, subject, sgsConfig)

	// Send the request with token to the server via kubectl
	SendRequest(request, token)
}
