package domain

import (
	"fmt"
	"os"
	"time"
)

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
	StatusCreating    EnvironmentStatus = "creating"
	StatusProvisioned EnvironmentStatus = "provisioned"
	StatusRunning     EnvironmentStatus = "running"
	StatusStopped     EnvironmentStatus = "stopped"
	StatusError       EnvironmentStatus = "error"
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

// SanitizeName replaces characters not safe for use in database names, file paths,
// and Docker Compose project names. Lowercase alphanumeric, hyphens, and underscores only.
func SanitizeName(name string) string {
	var b []byte
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
			b = append(b, c)
		case c >= 'A' && c <= 'Z':
			b = append(b, c+32) // lowercase
		default:
			b = append(b, '_')
		}
	}
	return string(b)
}

// ComposeProjectName returns the compose project name for this environment.
// Sanitizes to match Docker Compose requirements: lowercase alphanumeric, hyphens, underscores.
func ComposeProjectName(projectName, envName string) string {
	return SanitizeName(projectName + "-" + envName)
}


// StepRecordStatus represents the persisted outcome of a step.
type StepRecordStatus string

const (
	StepRecordCompleted StepRecordStatus = "completed"
	StepRecordFailed    StepRecordStatus = "failed"
)

// StepRecord is the checkpoint persisted for a single step execution.
type StepRecord struct {
	Name       string           `json:"name"`
	Status     StepRecordStatus `json:"status"`
	StartedAt  time.Time        `json:"startedAt"`
	FinishedAt time.Time        `json:"finishedAt"`
	DurationMs int64            `json:"durationMs"`
	Machine    string           `json:"machine"`
	Error      string           `json:"error,omitempty"`
	Outputs    map[string]any   `json:"outputs,omitempty"`
}

// AuditEntry is an append-only log entry recording what happened during lifecycle operations.
type AuditEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	Step       string    `json:"step"`
	Action     string    `json:"action"` // "executed", "skipped", "verified", "verify_failed", "invalidated", "failed"
	Machine    string    `json:"machine"`
	DurationMs int64     `json:"durationMs,omitempty"`
	Message    string    `json:"message,omitempty"`
	Error      string    `json:"error,omitempty"`
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
	Steps              map[string]*StepRecord       `json:"steps,omitempty"`
	AuditLog           []AuditEntry                 `json:"auditLog,omitempty"`
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

// StepCompleted returns true if the named step has a "completed" record.
func (e *EnvironmentEntry) StepCompleted(name string) bool {
	if e.Steps == nil {
		return false
	}
	r, ok := e.Steps[name]
	return ok && r.Status == StepRecordCompleted
}

// SetStepRecord records a step completion/failure.
func (e *EnvironmentEntry) SetStepRecord(rec *StepRecord) {
	if e.Steps == nil {
		e.Steps = make(map[string]*StepRecord)
	}
	e.Steps[rec.Name] = rec
	e.UpdatedAt = rec.FinishedAt
}

// AppendAudit adds an audit log entry.
func (e *EnvironmentEntry) AppendAudit(entry AuditEntry) {
	e.AuditLog = append(e.AuditLog, entry)
}

// StepOutputs returns the outputs map for a completed step, or nil.
func (e *EnvironmentEntry) StepOutputs(name string) map[string]any {
	if e.Steps == nil {
		return nil
	}
	r, ok := e.Steps[name]
	if !ok || r.Status != StepRecordCompleted {
		return nil
	}
	return r.Outputs
}

// InvalidateStepsFrom removes checkpoint records for the named step and all
// steps that come after it in the given ordered step list.
func (e *EnvironmentEntry) InvalidateStepsFrom(stepName string, orderedSteps []string) {
	found := false
	for _, s := range orderedSteps {
		if s == stepName {
			found = true
		}
		if found {
			delete(e.Steps, s)
			e.AppendAudit(AuditEntry{
				Timestamp: time.Now(),
				Step:      s,
				Action:    "invalidated",
				Machine:   Hostname(),
				Message:   fmt.Sprintf("Invalidated due to --from %s", stepName),
			})
		}
	}
}

// Hostname returns the machine hostname for audit logging.
func Hostname() string {
	h, _ := os.Hostname()
	if h == "" {
		return "unknown"
	}
	return h
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
