// Package sgs provides shared constants and types for the SGS CLI.
package sgs

// Label keys for Kubernetes resources
const (
	LabelManagedBy      = "app.kubernetes.io/managed-by"
	LabelManagedByValue = "sgs"
	LabelNodeName       = "sgs.snucse.org/node-name"
	LabelVolumeName     = "sgs.snucse.org/volume-name"
	LabelSessionMode    = "sgs.snucse.org/session-mode"
	LabelWorkspaceID    = "sgs.snucse.org/id"
)

// Annotation keys for Kubernetes resources
const (
	AnnotationSelectedNode = "volume.kubernetes.io/selected-node"
	AnnotationOSImage      = "sgs.snucse.org/os-image"
	AnnotationNodeSelector = "scheduler.alpha.kubernetes.io/node-selector"
)

// Session modes
const (
	SessionModeEdit = "edit"
	SessionModeRun  = "run"
)

// Default values
const (
	DefaultImage       = "nvcr.io/nvidia/cuda:12.5.0-base-ubuntu22.04"
	DefaultStorageSize = "10Gi"
	RuntimeClassName   = "sgs"
)

// Edit mode resource limits
const (
	EditCPULimit    = "4"
	EditMemoryLimit = "16Gi"
)

// Beacon mount path - the runtime wrapper detects this path to trigger root swap
const BeaconMount = "/sgs-os-volume"
