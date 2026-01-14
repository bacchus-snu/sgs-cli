// Package sgs provides shared configuration and types for the SGS CLI.
// Configuration is loaded from ~/.sgs/constants.yaml, which is downloaded
// by 'sgs fetch' from the GitHub repository.
package sgs

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// Config holds all SGS configuration values loaded from constants.yaml.
type Config struct {
	Labels       LabelsConfig       `yaml:"labels"`
	Annotations  AnnotationsConfig  `yaml:"annotations"`
	SessionModes SessionModesConfig `yaml:"sessionModes"`
	Defaults     DefaultsConfig     `yaml:"defaults"`
	EditLimits   EditLimitsConfig   `yaml:"editLimits"`
	BeaconMount  string             `yaml:"beaconMountPath"`
}

// LabelsConfig holds label key configurations.
type LabelsConfig struct {
	ManagedBy      string `yaml:"managedBy"`
	ManagedByValue string `yaml:"managedByValue"`
	NodeName       string `yaml:"nodeName"`
	VolumeName     string `yaml:"volumeName"`
	SessionMode    string `yaml:"sessionMode"`
	WorkspaceID    string `yaml:"workspaceID"`
}

// AnnotationsConfig holds annotation key configurations.
type AnnotationsConfig struct {
	SelectedNode string `yaml:"selectedNode"`
	OSImage      string `yaml:"osImage"`
	OSVolume     string `yaml:"osVolume"`
	NodeSelector string `yaml:"nodeSelector"`
}

// SessionModesConfig holds session mode values.
type SessionModesConfig struct {
	Edit string `yaml:"edit"`
	Run  string `yaml:"run"`
}

// DefaultsConfig holds default values for resource creation.
type DefaultsConfig struct {
	Image            string `yaml:"image"`
	StorageSize      string `yaml:"storageSize"`
	RuntimeClassName string `yaml:"runtimeClassName"`
}

// EditLimitsConfig holds edit mode resource limits.
type EditLimitsConfig struct {
	CPU    string `yaml:"cpu"`
	Memory string `yaml:"memory"`
}

var (
	globalConfig *Config
	configOnce   sync.Once
	configErr    error
)

// configPath returns the path to the constants config file.
func configPath() string {
	return filepath.Join(os.Getenv("HOME"), ".sgs", "constants.yaml")
}

// LoadConfig loads the configuration from ~/.sgs/constants.yaml.
// It uses sync.Once to ensure the config is loaded only once.
func LoadConfig() (*Config, error) {
	configOnce.Do(func() {
		globalConfig, configErr = loadConfigFromFile()
	})
	return globalConfig, configErr
}

// loadConfigFromFile reads and parses the constants.yaml file.
func loadConfigFromFile() (*Config, error) {
	configFile := configPath()

	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("configuration not found at %s, run 'sgs fetch' first", configFile)
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Validate required fields
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &config, nil
}

// validate checks that all required configuration fields are set.
func (c *Config) validate() error {
	if c.Labels.ManagedBy == "" {
		return fmt.Errorf("labels.managedBy is required")
	}
	if c.Labels.ManagedByValue == "" {
		return fmt.Errorf("labels.managedByValue is required")
	}
	if c.Labels.NodeName == "" {
		return fmt.Errorf("labels.nodeName is required")
	}
	if c.Labels.VolumeName == "" {
		return fmt.Errorf("labels.volumeName is required")
	}
	if c.Labels.SessionMode == "" {
		return fmt.Errorf("labels.sessionMode is required")
	}
	if c.Annotations.SelectedNode == "" {
		return fmt.Errorf("annotations.selectedNode is required")
	}
	if c.Annotations.OSImage == "" {
		return fmt.Errorf("annotations.osImage is required")
	}
	if c.Annotations.OSVolume == "" {
		return fmt.Errorf("annotations.osVolume is required")
	}
	if c.SessionModes.Edit == "" {
		return fmt.Errorf("sessionModes.edit is required")
	}
	if c.SessionModes.Run == "" {
		return fmt.Errorf("sessionModes.run is required")
	}
	if c.Defaults.Image == "" {
		return fmt.Errorf("defaults.image is required")
	}
	if c.Defaults.StorageSize == "" {
		return fmt.Errorf("defaults.storageSize is required")
	}
	if c.Defaults.RuntimeClassName == "" {
		return fmt.Errorf("defaults.runtimeClassName is required")
	}
	if c.EditLimits.CPU == "" {
		return fmt.Errorf("editLimits.cpu is required")
	}
	if c.EditLimits.Memory == "" {
		return fmt.Errorf("editLimits.memory is required")
	}
	return nil
}

// MustLoadConfig loads the configuration and panics on error.
// Use this only in contexts where configuration must exist.
func MustLoadConfig() *Config {
	config, err := LoadConfig()
	if err != nil {
		panic(fmt.Sprintf("failed to load SGS config: %v", err))
	}
	return config
}

// ResetConfig clears the cached configuration, forcing a reload on next access.
// This is useful after 'sgs fetch' updates the constants.yaml file.
func ResetConfig() {
	configOnce = sync.Once{}
	globalConfig = nil
	configErr = nil
}

// GetConfig returns the loaded configuration or an error.
// This is the primary way to access configuration values.
func GetConfig() (*Config, error) {
	return LoadConfig()
}
