package main

import (
	"flag"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type SGSConfig struct {
	// Common fields
	Name      string `yaml:"name"`
	Server    string `yaml:"server"`
	Workspace string `yaml:"workspace"`

	// Job specific fields
	Volume     string            `yaml:"volume"`
	GPU        string            `yaml:"gpu"`
	Image      string            `yaml:"image"`
	WorkingDir string            `yaml:"workingDir"`
	Command    []string          `yaml:"command"`
	Env        map[string]string `yaml:"env"`
	Secret     string            `yaml:"secret"`

	// Volume specific fields
	Size string `yaml:"size"`

	// Job & Volume specific fields
	CPU    string `yaml:"cpu"`
	Memory string `yaml:"memory"`

	// Secret specific fields
	Data map[string]string `yaml:"data"`
}

func ParseSGSConfig() (string, string, SGSConfig) {
	// TODO: flag cannot handle the case where behavior and subject are provided before the flags

	// Read the name from command line flag
	name := flag.String("n", "", "Name")

	// Read the server from command line flag
	server := flag.String("s", "", "Server")

	// Read the workspace from command line flag
	workspace := flag.String("w", "", "Workspace")

	// Read the file name from command line flag
	fileName := flag.String("f", "", "Path to the YAML file")

	flag.Parse()

	// Read non-flag command line arguments
	args := flag.Args()

	// Check the number of command line arguments
	if len(args) != 2 {
		log.Fatalf("Usage: %s <behavior> <subject>", os.Args[0])
	}

	// Read the behavior and subject from command line arguments
	behavior := args[0]
	subject := args[1]

	// Read the YAML file
	yamlFile, err := os.ReadFile(*fileName)
	if err != nil {
		log.Fatalf("Failed to read YAML file: %v", err)
	}

	// Parse the YAML data into a struct
	var sgsConfig SGSConfig
	err = yaml.Unmarshal(yamlFile, &sgsConfig)
	if err != nil {
		log.Fatalf("Failed to parse YAML: %v", err)
	}

	// Override the name if provided
	if *name != "" {
		sgsConfig.Name = *name
	}

	// Override the server if provided
	if *server != "" {
		sgsConfig.Server = *server
	}

	// Override the workspace if provided
	if *workspace != "" {
		sgsConfig.Workspace = *workspace
	}

	return behavior, subject, sgsConfig
}

func CheckSGSConfig(behavior string, subject string, sgsConfig SGSConfig) {
	// Check the common fields
	if sgsConfig.Name == "" {
		log.Fatalf("Name is required")
	}
	if sgsConfig.Server == "" {
		log.Fatalf("Server is required")
	}
	if sgsConfig.Workspace == "" {
		log.Fatalf("Workspace is required")
	}

	// Check the behavior
	switch behavior {
	case "create":
		switch subject {
		case "job":
			if sgsConfig.Volume == "" {
				log.Fatalf("Volume is required for creating a job")
			}
			if sgsConfig.Image == "" {
				log.Fatalf("Image is required for creating a job")
			}
			if len(sgsConfig.Command) == 0 {
				log.Fatalf("Command is required for creating a job")
			}
		case "volume":
			if sgsConfig.Size == "" {
				log.Fatalf("Size is required for creating a volume")
			}
		case "secret":
			if len(sgsConfig.Data) == 0 {
				log.Fatalf("Data is required for creating a secret")
			}
		default:
			log.Fatalf("Invalid subject: %s", subject)
		}
	case "delete":
		// No additional fields are required for deleting a job, volume, or secret
		switch subject {
		case "job":
		case "volume":
		case "secret":
		default:
			log.Fatalf("Invalid subject: %s", subject)
		}
	case "log":
		switch subject {
		case "job":
		default:
			log.Fatalf("Invalid subject: %s", subject)
		}
	case "connect":
		switch subject {
		case "volume":
		default:
			log.Fatalf("Invalid subject: %s", subject)
		}
	default:
		log.Fatalf("Invalid behavior: %s", behavior)
	}
}
