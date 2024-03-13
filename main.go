package main

import (
	"flag"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
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

func main() {
	// Read the name from command line flag
	name := flag.String("n", "", "Name")

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
	var config Config
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		log.Fatalf("Failed to parse YAML: %v", err)
	}

	// Override the name if provided
	if *name != "" {
		config.Name = *name
	}

	log.Printf("%+v", config)

	// Perform behavior based on subject
	switch behavior {
	case "create":
		switch subject {
		case "job":
			// TODO: Implement create job logic
		case "volume":
			// TODO: Implement create volume logic
		case "secret":
			// TODO: Implement create secret logic
		default:
			log.Fatalf("Invalid subject: %s", subject)
		}
	case "delete":
		switch subject {
		case "job":
			// TODO: Implement delete job logic
		case "volume":
			// TODO: Implement delete volume logic
		case "secret":
			// TODO: Implement delete secret logic
		default:
			log.Fatalf("Invalid subject: %s", subject)
		}
	case "log":
		switch subject {
		case "job":
			// TODO: Implement log job logic
		default:
			log.Fatalf("Invalid subject: %s", subject)
		}
	case "connect":
		switch subject {
		case "volume":
			// TODO: Implement connect volume logic
		default:
			log.Fatalf("Invalid subject: %s", subject)
		}
	default:
		log.Fatalf("Invalid behavior: %s", behavior)
	}
}
