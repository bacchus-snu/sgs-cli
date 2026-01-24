package sgs

// Package-level variables that hold configuration values.
// These are populated on first access via the accessor functions,
// falling back to defaults if the config file is not available.
//
// NOTE: These variables provide backward compatibility with code
// that expects constant-style access patterns. For new code,
// consider using the accessor functions (Labels(), Annotations(), etc.)
// which provide more flexibility.

var (
	// Label keys
	LabelManagedBy      = "app.kubernetes.io/managed-by"
	LabelManagedByValue = "sgs"
	LabelNodeName       = "sgs.snucse.org/node-name"
	LabelVolumeName     = "sgs.snucse.org/volume-name"
	LabelSessionMode    = "sgs.snucse.org/session-mode"
	LabelWorkspaceID    = "sgs.snucse.org/id"

	// Annotation keys
	AnnotationSelectedNode = "volume.kubernetes.io/selected-node"
	AnnotationOSImage      = "sgs.snucse.org/os-image"
	AnnotationNodeSelector = "scheduler.alpha.kubernetes.io/node-selector"

	// Session modes
	SessionModeEdit = "edit"
	SessionModeRun  = "run"

	// Defaults
	DefaultImage       = "nvcr.io/nvidia/cuda:12.5.0-base-ubuntu22.04"
	DefaultStorageSize = "10Gi"
	RuntimeClassName   = "sgs"

	// Edit limits
	EditCPULimit    = "4"
	EditMemoryLimit = "16Gi"

	// Beacon mount path - the runtime wrapper detects this path to trigger root swap
	BeaconMount = "/sgs-os-volume"
)

// InitFromConfig loads configuration from the config file and updates
// the package-level variables. This should be called once at startup,
// typically after EnsureConfig() succeeds. If the config file doesn't
// exist or is invalid, the default values are preserved.
func InitFromConfig() error {
	cfg, err := LoadConfig()
	if err != nil {
		// Config not available, keep defaults
		return nil
	}

	// Update label keys
	LabelManagedBy = cfg.Labels.ManagedBy
	LabelManagedByValue = cfg.Labels.ManagedByValue
	LabelNodeName = cfg.Labels.NodeName
	LabelVolumeName = cfg.Labels.VolumeName
	LabelSessionMode = cfg.Labels.SessionMode
	LabelWorkspaceID = cfg.Labels.WorkspaceID

	// Update annotation keys
	AnnotationSelectedNode = cfg.Annotations.SelectedNode
	AnnotationOSImage = cfg.Annotations.OSImage
	AnnotationNodeSelector = cfg.Annotations.NodeSelector

	// Update session modes
	SessionModeEdit = cfg.SessionModes.Edit
	SessionModeRun = cfg.SessionModes.Run

	// Update defaults
	DefaultImage = cfg.Defaults.Image
	DefaultStorageSize = cfg.Defaults.StorageSize
	RuntimeClassName = cfg.Defaults.RuntimeClassName

	// Update edit limits
	EditCPULimit = cfg.EditLimits.CPU
	EditMemoryLimit = cfg.EditLimits.Memory

	// Update beacon mount
	BeaconMount = cfg.BeaconMount

	return nil
}

// Accessor functions that load values from the configuration file.
// These provide access to configuration categories.

// Labels returns the labels configuration.
func Labels() LabelsConfig {
	cfg, err := GetConfig()
	if err != nil {
		return LabelsConfig{
			ManagedBy:      LabelManagedBy,
			ManagedByValue: LabelManagedByValue,
			NodeName:       LabelNodeName,
			VolumeName:     LabelVolumeName,
			SessionMode:    LabelSessionMode,
			WorkspaceID:    LabelWorkspaceID,
		}
	}
	return cfg.Labels
}

// Annotations returns the annotations configuration.
func Annotations() AnnotationsConfig {
	cfg, err := GetConfig()
	if err != nil {
		return AnnotationsConfig{
			SelectedNode: AnnotationSelectedNode,
			OSImage:      AnnotationOSImage,
			NodeSelector: AnnotationNodeSelector,
		}
	}
	return cfg.Annotations
}

// SessionModes returns the session modes configuration.
func SessionModes() SessionModesConfig {
	cfg, err := GetConfig()
	if err != nil {
		return SessionModesConfig{
			Edit: SessionModeEdit,
			Run:  SessionModeRun,
		}
	}
	return cfg.SessionModes
}

// Defaults returns the defaults configuration.
func Defaults() DefaultsConfig {
	cfg, err := GetConfig()
	if err != nil {
		return DefaultsConfig{
			Image:            DefaultImage,
			StorageSize:      DefaultStorageSize,
			RuntimeClassName: RuntimeClassName,
		}
	}
	return cfg.Defaults
}

// EditLimits returns the edit limits configuration.
func EditLimits() EditLimitsConfig {
	cfg, err := GetConfig()
	if err != nil {
		return EditLimitsConfig{
			CPU:    EditCPULimit,
			Memory: EditMemoryLimit,
		}
	}
	return cfg.EditLimits
}

// BeaconMountPath returns the beacon mount path.
func BeaconMountPath() string {
	cfg, err := GetConfig()
	if err != nil {
		return BeaconMount
	}
	return cfg.BeaconMount
}
