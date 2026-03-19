package domain

// StepStatus represents the status of a lifecycle step.
type StepStatus string

const (
	StepStarted   StepStatus = "started"
	StepCompleted StepStatus = "completed"
	StepFailed    StepStatus = "failed"
	StepSkipped   StepStatus = "skipped"
	StepStreaming StepStatus = "streaming" // stop spinner, hook will stream its own output
)

// StepEvent is emitted by the manager at each lifecycle transition.
type StepEvent struct {
	Step    string // e.g. "allocate_ports", "create_worktree", "create_database"
	Status  StepStatus
	Message string // human-readable detail
	Error   error  // non-nil when Status == StepFailed
}
