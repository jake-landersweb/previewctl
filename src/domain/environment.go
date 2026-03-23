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
	StatusCreating     EnvironmentStatus = "creating"
	StatusProvisioned  EnvironmentStatus = "provisioned"
	StatusRunning      EnvironmentStatus = "running"
	StatusStopped      EnvironmentStatus = "stopped"
	StatusError        EnvironmentStatus = "error"
)

// PortMap maps service names to allocated ports.
type PortMap map[string]int

// ComputeResources holds the result of creating compute resources for an environment.
type ComputeResources struct {
	WorktreePath string `json:"worktreePath,omitempty"` // local mode
	VMId         string `json:"vmId,omitempty"`         // preview/sandbox mode
	ExternalIP   string `json:"externalIp,omitempty"`   // preview/sandbox mode
}

// ComputeAccessInfo stores how to reach the compute location so environments
// can be reconnected across CLI invocations.
type ComputeAccessInfo struct {
	Type            string `json:"type"`                      // "local" or "ssh"
	Path            string `json:"path,omitempty"`            // local worktree path
	Host            string `json:"host,omitempty"`            // VM IP (ssh)
	User            string `json:"user,omitempty"`            // SSH user
	ManagedWorktree bool   `json:"managedWorktree,omitempty"` // true = created by previewctl
}

// ComposeProjectName returns the compose project name for this environment.
func ComposeProjectName(projectName, envName string) string {
	return projectName + "-" + envName
}

// EnvironmentEntry is a tracked environment persisted in state.
type EnvironmentEntry struct {
	Name               string                       `json:"name"`
	Mode               EnvironmentMode              `json:"mode"`
	Branch             string                       `json:"branch"`
	Status             EnvironmentStatus            `json:"status"`
	CreatedAt          time.Time                    `json:"createdAt"`
	UpdatedAt          time.Time                    `json:"updatedAt"`
	Ports              PortMap                      `json:"ports"`
	ProvisionerOutputs map[string]map[string]string `json:"provisionerOutputs"`
	Compute            *ComputeAccessInfo           `json:"compute,omitempty"`
}

// WorktreePath returns the worktree path from ComputeAccessInfo, or empty string.
func (e *EnvironmentEntry) WorktreePath() string {
	if e.Compute != nil {
		return e.Compute.Path
	}
	return ""
}

// IsManagedWorktree returns whether the worktree was created by previewctl.
func (e *EnvironmentEntry) IsManagedWorktree() bool {
	return e.Compute != nil && e.Compute.ManagedWorktree
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
