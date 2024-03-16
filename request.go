package main

import (
	"log"
	"os"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/component-base/cli"
	"k8s.io/kubectl/pkg/cmd"
	"k8s.io/kubectl/pkg/cmd/plugin"
	"k8s.io/kubectl/pkg/cmd/util"
)

type JobRequest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Spec struct {
		RestartPolicy string `yaml:"restartPolicy"`
		Containers    []struct {
			Name    string   `yaml:"name"`
			Image   string   `yaml:"image"`
			Command []string `yaml:"command"`
			Env     []struct {
				Name  string `yaml:"name"`
				Value string `yaml:"value"`
			} `yaml:"env"`
			WorkingDir string `yaml:"workingDir"`
			Resources  struct {
				Limits struct {
					CPU    string `yaml:"cpu"`
					Memory string `yaml:"memory"`
					GPU    string `yaml:"nvidia.com/gpu"`
				} `yaml:"limits"`
				Requests struct {
					CPU    string `yaml:"cpu"`
					Memory string `yaml:"memory"`
					GPU    string `yaml:"nvidia.com/gpu"`
				} `yaml:"requests"`
			} `yaml:"resources"`
		} `yaml:"containers"`
		NodeSelector struct {
			Hostname string `yaml:"hostname"`
		} `yaml:"nodeSelector"`
		ImagePullSecrets []struct {
			Name string `yaml:"name"`
		} `yaml:"imagePullSecrets"`
		Volumes []struct {
			Name                  string `yaml:"name"`
			PersistentVolumeClaim struct {
				ClaimName string `yaml:"claimName"`
			} `yaml:"persistentVolumeClaim"`
		} `yaml:"volumes"`
	} `yaml:"spec"`
}

type VolumeRequest struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
	} `yaml:"metadata"`
	Spec struct {
		AccessModes []string `yaml:"accessModes"`
		Resources   struct {
			Requests struct {
				Storage string `yaml:"storage"`
			} `yaml:"requests"`
		} `yaml:"resources"`
	} `yaml:"spec"`
}

func SendRequest(behavior string, subject string, sgsConfig SGSConfig, token string) {
	// TODO: Implement the SendRequest function

	// Create a pipe
	r, w, err := os.Pipe()
	if err != nil {
		log.Fatalf("Failed to create pipe: %v", err)
	}

	// Create arguments for the request
	args := []string{
		"kubectl",
		"apply",
		"-f",
		"-",
		"--token",
		token,
	}

	// Create Request object

	// Write the request to the pipe
	w.Write([]byte("apiVersion: v1\n"))

	// Create a Cmd instance
	ioStreams := genericiooptions.IOStreams{In: r, Out: os.Stdout, ErrOut: os.Stderr}
	command := cmd.NewDefaultKubectlCommandWithArgs(cmd.KubectlOptions{
		PluginHandler: cmd.NewDefaultPluginHandler(plugin.ValidPluginFilenamePrefixes),
		Arguments:     args,
		ConfigFlags:   genericclioptions.NewConfigFlags(true).WithDeprecatedPasswordFlag().WithDiscoveryBurst(300).WithDiscoveryQPS(50.0).WithWarningPrinter(ioStreams),
		IOStreams:     ioStreams,
	})
	if err := cli.RunNoErrOutput(command); err != nil {
		// Pretty-print the error and exit with an error.
		util.CheckErr(err)
	}
}
