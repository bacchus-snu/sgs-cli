package client

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

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
		// Check if it's a connection reset error - retry
		// Otherwise, don't retry
		if !isRetryableError(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// isRetryableError checks if the error is retryable (connection reset, etc.)
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Connection reset by peer, connection refused, etc.
	return contains(errStr, "connection reset by peer") ||
		contains(errStr, "connection refused") ||
		contains(errStr, "EOF") ||
		contains(errStr, "no such host") ||
		contains(errStr, "i/o timeout")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// configPath returns the path to the SGS config file
func configPath() string {
	return filepath.Join(os.Getenv("HOME"), ".sgs", "config.yaml")
}

// configURL is the URL to download the kubeconfig template from
const configURL = "https://raw.githubusercontent.com/bacchus-snu/sgs/refs/heads/master/controller/kubeconfig.template"

// EnsureConfig checks if config exists, if not, fetches it
func EnsureConfig() error {
	configFile := configPath()

	// Check if config already exists
	if _, err := os.Stat(configFile); err == nil {
		return nil // Config exists
	}

	// Config doesn't exist, fetch it
	return FetchConfig()
}

// FetchConfig downloads the configuration file from GitHub and applies modifications
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

	fmt.Printf("Downloading configuration from %s...\n", configURL)

	// Download the config template
	resp, err := http.Get(configURL)
	if err != nil {
		return fmt.Errorf("failed to download config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download config: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read config response: %w", err)
	}

	// Apply modifications to the template
	content := string(data)

	// Remove -{{ . }} postfix from contexts.name and current-context
	content = strings.ReplaceAll(content, "snucse-sommelier-{{ . }}", "snucse-sommelier")

	// Remove the namespace line
	content = strings.ReplaceAll(content, "      namespace: ws-{{ . }}\n", "")

	// Add token-cache-dir after oidc-extra-scope=groups
	content = strings.ReplaceAll(content,
		"          - --oidc-extra-scope=groups",
		"          - --oidc-extra-scope=groups\n          - --token-cache-dir=~/.sgs/cache")

	if err := os.WriteFile(configFile, []byte(content), 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Configuration saved to %s\n", configFile)
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

	// Create clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

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
			context["namespace"] = workspace
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
