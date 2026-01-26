// Package client provides the Kubernetes client wrapper for SGS operations.
// It handles kubeconfig loading, authentication, and provides retry logic
// for transient network errors.
package client

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/bacchus-snu/sgs-cli/internal/sgs"
	"gopkg.in/yaml.v3"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client wraps the Kubernetes client with SGS-specific functionality
type Client struct {
	Clientset *kubernetes.Clientset
	Config    *rest.Config
	Namespace string
}

// retryRoundTripper wraps an http.RoundTripper with retry logic
type retryRoundTripper struct {
	delegate   http.RoundTripper
	maxRetries int
	retryDelay time.Duration
}

func (r *retryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Read and buffer the body if present (for retries)
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(r.retryDelay)
		}

		// Clone the request and restore body for each attempt
		reqCopy := req.Clone(req.Context())
		if bodyBytes != nil {
			reqCopy.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			reqCopy.ContentLength = int64(len(bodyBytes))
		}

		resp, err := r.delegate.RoundTrip(reqCopy)
		if err == nil {
			return resp, nil
		}

		lastErr = err
		// Use shared retry logic
		if !IsRetryableError(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// configPath returns the path to the SGS config file
func configPath() string {
	return filepath.Join(os.Getenv("HOME"), ".sgs", "config.yaml")
}

// metadataPath returns the path to the SGS metadata file
func metadataPath() string {
	return filepath.Join(os.Getenv("HOME"), ".sgs", "metadata.yaml")
}

// Metadata stores CLI metadata like last fetch time
type Metadata struct {
	LastFetched time.Time `yaml:"last_fetched"`
}

// getLastFetched reads the last fetch time from metadata
func getLastFetched() (time.Time, error) {
	data, err := os.ReadFile(metadataPath())
	if err != nil {
		return time.Time{}, err
	}

	var metadata Metadata
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		return time.Time{}, err
	}

	return metadata.LastFetched, nil
}

// setLastFetched writes the current time to metadata
func setLastFetched() error {
	metadata := Metadata{
		LastFetched: time.Now(),
	}

	data, err := yaml.Marshal(metadata)
	if err != nil {
		return err
	}

	return os.WriteFile(metadataPath(), data, 0600)
}

// shouldAutoFetch returns true if 7+ days have passed since last fetch
func shouldAutoFetch() bool {
	lastFetched, err := getLastFetched()
	if err != nil {
		// If we can't read metadata, don't auto-fetch
		return false
	}

	return time.Since(lastFetched) >= 7*24*time.Hour
}

// configURL is the URL to download the kubeconfig from
const configURL = "https://raw.githubusercontent.com/bacchus-snu/sgs/refs/heads/master/controller/config.yaml"

// EnsureConfig checks if config exists, if not, fetches it.
// Also auto-fetches if 7+ days have passed since last fetch.
func EnsureConfig() error {
	configFile := configPath()

	if _, err := os.Stat(configFile); err != nil {
		// Config doesn't exist, fetch it
		return FetchConfig()
	}

	// Check if we should auto-fetch (7+ days since last fetch)
	if shouldAutoFetch() {
		fmt.Println("Configuration is older than 7 days, refreshing...")
		if err := FetchConfig(); err != nil {
			// Don't fail if auto-fetch fails, just warn
			fmt.Printf("Warning: failed to refresh configuration: %v\n", err)
		} else {
			// Check for CLI updates (same as 'sgs fetch')
			PromptForUpdate()
		}
	}

	return nil
}

// FetchConfig downloads the kubeconfig from GitHub
func FetchConfig() error {
	configFile := configPath()

	// Create directory if it doesn't exist
	configDir := filepath.Dir(configFile)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Also create cache directory for token cache
	cacheDir := filepath.Join(configDir, "cache")
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Download kubeconfig
	fmt.Println("Downloading cluster configuration from server...")

	resp, err := http.Get(configURL)
	if err != nil {
		return fmt.Errorf("failed to download kubeconfig: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download kubeconfig: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read kubeconfig response: %w", err)
	}

	// Write the config
	if err := os.WriteFile(configFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write kubeconfig: %w", err)
	}

	fmt.Println("Cluster configuration saved.")

	// Update last fetched timestamp
	if err := setLastFetched(); err != nil {
		// Don't fail if we can't write metadata
		fmt.Printf("Warning: failed to update metadata: %v\n", err)
	}

	return nil
}

// New creates a new SGS client using the kubeconfig at ~/.sgs/config.yaml
func New() (*Client, error) {
	// Ensure config exists
	if err := EnsureConfig(); err != nil {
		return nil, err
	}

	kubeconfigPath := configPath()

	// Load kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	// Increase QPS and Burst to avoid client-side throttling
	// when making multiple parallel requests
	config.QPS = 100
	config.Burst = 200

	// Wrap the transport with retry logic
	config.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return &retryRoundTripper{
			delegate:   rt,
			maxRetries: 1, // Retry once (total 2 attempts)
			retryDelay: 500 * time.Millisecond,
		}
	})

	// Suppress stderr BEFORE creating clientset.
	// The OIDC credential plugin (kubelogin) writes transient errors to stderr.
	// client-go captures os.Stderr when creating the exec authenticator,
	// so we must redirect BEFORE NewForConfig.
	origStderr := os.Stderr
	devNull, nullErr := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if nullErr == nil {
		os.Stderr = devNull
	}

	// Create clientset (this captures os.Stderr for the exec credential plugin)
	clientset, err := kubernetes.NewForConfig(config)

	// Restore stderr after clientset creation
	if nullErr == nil {
		os.Stderr = origStderr
		devNull.Close()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Warm up authentication by making a simple API call.
	// This triggers the OIDC credential plugin and caches the token.
	// Stderr was already suppressed when the authenticator was created.
	warmupAuthentication(clientset)

	// Get current namespace from kubeconfig
	namespace, err := getCurrentNamespace(kubeconfigPath)
	if err != nil {
		namespace = "default"
	}

	return &Client{
		Clientset: clientset,
		Config:    config,
		Namespace: namespace,
	}, nil
}

// getCurrentNamespace reads the current namespace from the kubeconfig
func getCurrentNamespace(kubeconfigPath string) (string, error) {
	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return "", err
	}

	var kubeconfig struct {
		CurrentContext string `yaml:"current-context"`
		Contexts       []struct {
			Name    string `yaml:"name"`
			Context struct {
				Namespace string `yaml:"namespace"`
			} `yaml:"context"`
		} `yaml:"contexts"`
	}

	if err := yaml.Unmarshal(data, &kubeconfig); err != nil {
		return "", err
	}

	for _, ctx := range kubeconfig.Contexts {
		if ctx.Name == kubeconfig.CurrentContext {
			if ctx.Context.Namespace != "" {
				return ctx.Context.Namespace, nil
			}
			return "default", nil
		}
	}

	return "default", nil
}

// IsNamespaceExplicitlySet checks if the namespace was explicitly set in kubeconfig
func IsNamespaceExplicitlySet() bool {
	kubeconfigPath := configPath()

	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return false
	}

	var kubeconfig struct {
		CurrentContext string `yaml:"current-context"`
		Contexts       []struct {
			Name    string `yaml:"name"`
			Context struct {
				Namespace string `yaml:"namespace"`
			} `yaml:"context"`
		} `yaml:"contexts"`
	}

	if err := yaml.Unmarshal(data, &kubeconfig); err != nil {
		return false
	}

	for _, ctx := range kubeconfig.Contexts {
		if ctx.Name == kubeconfig.CurrentContext {
			return ctx.Context.Namespace != ""
		}
	}

	return false
}

// SetWorkspace updates the namespace in the kubeconfig file
func SetWorkspace(workspace string) error {
	kubeconfigPath := configPath()

	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	// Parse the kubeconfig
	var kubeconfig map[string]interface{}
	if err := yaml.Unmarshal(data, &kubeconfig); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Get current context
	currentContext, ok := kubeconfig["current-context"].(string)
	if !ok {
		return fmt.Errorf("current-context not found in config")
	}

	// Update the namespace in the current context
	contexts, ok := kubeconfig["contexts"].([]interface{})
	if !ok {
		return fmt.Errorf("contexts not found in config")
	}

	for _, ctx := range contexts {
		ctxMap, ok := ctx.(map[string]interface{})
		if !ok {
			continue
		}
		if ctxMap["name"] == currentContext {
			context, ok := ctxMap["context"].(map[string]interface{})
			if !ok {
				context = make(map[string]interface{})
				ctxMap["context"] = context
			}
			context["namespace"] = sgs.WorkspaceToNamespace(workspace)
			break
		}
	}

	// Write back the config
	newData, err := yaml.Marshal(kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(kubeconfigPath, newData, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// SetMode switches between production and development clusters
func SetMode(mode string) error {
	kubeconfigPath := configPath()

	data, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	// Parse the kubeconfig
	var kubeconfig map[string]interface{}
	if err := yaml.Unmarshal(data, &kubeconfig); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Set the current-context based on mode
	switch mode {
	case "prod":
		kubeconfig["current-context"] = "sgs"
	case "dev":
		kubeconfig["current-context"] = "sgs-dev"
	default:
		return fmt.Errorf("invalid mode: %s (must be 'prod' or 'dev')", mode)
	}

	// Write back the config
	newData, err := yaml.Marshal(kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(kubeconfigPath, newData, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// warmupAuthentication makes a simple API call to trigger and cache authentication.
// This ensures the token is cached so subsequent calls don't need to re-authenticate.
func warmupAuthentication(clientset *kubernetes.Clientset) {
	// Make a simple API call to trigger authentication
	// We use ServerVersion which is lightweight and doesn't require any permissions
	// Retry a few times to handle transient network errors
	for i := 0; i < 3; i++ {
		_, err := clientset.Discovery().ServerVersion()
		if err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}
