package domain

import "time"

// EnvironmentMode represents the deployment mode of an environment.
type EnvironmentMode string

const (
	ModeLocal   EnvironmentMode = "local"
	ModePreview EnvironmentMode = "preview"
	ModeSandbox EnvironmentMode = "sandbox"
)

// EnvironmentStatus represents the lifecycle status of an environment.
type EnvironmentStatus string

const (
	StatusCreating EnvironmentStatus = "creating"
	StatusRunning  EnvironmentStatus = "running"
	StatusStopped  EnvironmentStatus = "stopped"
	StatusError    EnvironmentStatus = "error"
)

// PortMap maps service names to allocated ports.
type PortMap map[string]int

// ComputeResources holds the result of creating compute resources for an environment.
type ComputeResources struct {
	WorktreePath string `json:"worktreePath,omitempty"` // local mode
	VMId         string `json:"vmId,omitempty"`         // preview/sandbox mode
	ExternalIP   string `json:"externalIp,omitempty"`   // preview/sandbox mode
}

// LocalMeta holds local-mode specific metadata.
type LocalMeta struct {
	WorktreePath       string `json:"worktreePath"`
	ComposeProjectName string `json:"composeProjectName"`
	ManagedWorktree    bool   `json:"managedWorktree"` // true = created by previewctl, false = attached
}

// RemoteMeta holds remote-mode specific metadata (future).
type RemoteMeta struct {
	VMId       string `json:"vmId"`
	ExternalIP string `json:"externalIp"`
}

// EnvironmentEntry is a tracked environment persisted in state.
type EnvironmentEntry struct {
	Name        string                       `json:"name"`
	Mode        EnvironmentMode              `json:"mode"`
	Branch      string                       `json:"branch"`
	Status      EnvironmentStatus            `json:"status"`
	CreatedAt   time.Time                    `json:"createdAt"`
	UpdatedAt   time.Time                    `json:"updatedAt"`
	Ports       PortMap                      `json:"ports"`
	ProvisionerOutputs map[string]map[string]string `json:"provisionerOutputs"`
	Local       *LocalMeta                   `json:"local,omitempty"`
	Remote      *RemoteMeta                  `json:"remote,omitempty"`
}

// EnvironmentDetail is an enriched view with live infrastructure checks.
type EnvironmentDetail struct {
	Entry        *EnvironmentEntry `json:"entry"`
	InfraRunning bool              `json:"infraRunning"`
}

// State is the top-level persisted state.
type State struct {
	Version      int                          `json:"version"`
	Environments map[string]*EnvironmentEntry `json:"environments"`
}

// NewState returns an initialized empty state.
func NewState() *State {
	return &State{
		Version:      1,
		Environments: make(map[string]*EnvironmentEntry),
	}
}

// VMSpec defines requirements for provisioning a VM (future).
type VMSpec struct {
	MachineType string
	DiskSizeGB  int
	Image       string
	Region      string
}

// VMInfo describes a provisioned VM (future).
type VMInfo struct {
	ID         string
	ExternalIP string
	Status     string
}
