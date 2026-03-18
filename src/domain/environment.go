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

// DatabaseInfo holds connection details for a database instance.
type DatabaseInfo struct {
	Host             string `json:"host"`
	Port             int    `json:"port"`
	User             string `json:"user"`
	Password         string `json:"password"`
	Database         string `json:"database"`
	ConnectionString string `json:"connectionString"`
}

// ComputeResources holds the result of creating compute resources for an environment.
type ComputeResources struct {
	WorktreePath string `json:"worktreePath,omitempty"` // local mode
	VMId         string `json:"vmId,omitempty"`         // preview/sandbox mode
	ExternalIP   string `json:"externalIp,omitempty"`   // preview/sandbox mode
}

// DatabaseRef is a lightweight reference to a database stored in state.
type DatabaseRef struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

// LocalMeta holds local-mode specific metadata.
type LocalMeta struct {
	WorktreePath       string `json:"worktreePath"`
	ComposeProjectName string `json:"composeProjectName"`
}

// RemoteMeta holds remote-mode specific metadata (future).
type RemoteMeta struct {
	VMId       string `json:"vmId"`
	ExternalIP string `json:"externalIp"`
}

// EnvironmentEntry is a tracked environment persisted in state.
type EnvironmentEntry struct {
	Name      string                 `json:"name"`
	Mode      EnvironmentMode        `json:"mode"`
	Branch    string                 `json:"branch"`
	Status    EnvironmentStatus      `json:"status"`
	CreatedAt time.Time              `json:"createdAt"`
	UpdatedAt time.Time              `json:"updatedAt"`
	Ports     PortMap                `json:"ports"`
	Databases map[string]DatabaseRef `json:"databases"`
	Local     *LocalMeta             `json:"local,omitempty"`
	Remote    *RemoteMeta            `json:"remote,omitempty"`
}

// EnvironmentDetail is an enriched view with live infrastructure checks.
type EnvironmentDetail struct {
	Entry          *EnvironmentEntry          `json:"entry"`
	InfraRunning   bool                       `json:"infraRunning"`
	DatabaseExists map[string]bool            `json:"databaseExists"`
	SnapshotInfo   map[string]*SnapshotState  `json:"snapshotInfo"`
}

// State is the top-level persisted state.
type State struct {
	Version      int                         `json:"version"`
	Snapshots    map[string]*SnapshotState   `json:"snapshots"`
	Environments map[string]*EnvironmentEntry `json:"environments"`
}

// NewState returns an initialized empty state.
func NewState() *State {
	return &State{
		Version:      1,
		Snapshots:    make(map[string]*SnapshotState),
		Environments: make(map[string]*EnvironmentEntry),
	}
}

// SnapshotState tracks the seeding status of a database template.
type SnapshotState struct {
	LastSeeded    *time.Time `json:"lastSeeded"`
	TemplateReady bool       `json:"templateReady"`
}

// SnapshotUpdate holds fields to update on snapshot state.
type SnapshotUpdate struct {
	LastSeeded    *time.Time
	TemplateReady *bool
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
