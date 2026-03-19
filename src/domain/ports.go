package domain

import "context"

// DatabasePort manages per-environment database lifecycle.
// Local: template cloning on shared Postgres.
// Preview/Sandbox: Neon branching, RDS snapshot, or full pg_dump restore.
type DatabasePort interface {
	// EnsureInfrastructure ensures the database server is running and reachable.
	EnsureInfrastructure(ctx context.Context) error

	// SeedTemplate populates the template database from resolved seed materials.
	SeedTemplate(ctx context.Context, materials []*SeedMaterial) error

	// CreateDatabase creates an isolated database for the given environment.
	CreateDatabase(ctx context.Context, envName string) (*DatabaseInfo, error)

	// DestroyDatabase drops the environment's database.
	DestroyDatabase(ctx context.Context, envName string) error

	// ResetDatabase drops and re-creates the environment's database.
	ResetDatabase(ctx context.Context, envName string) (*DatabaseInfo, error)

	// DatabaseExists checks if the environment's database exists.
	DatabaseExists(ctx context.Context, envName string) (bool, error)
}

// ComputePort manages the compute substrate for an environment.
// Local: git worktree + docker compose for per-env infrastructure.
// Preview: VM provisioning + full compose stack.
// Sandbox: isolated VM with network policies.
type ComputePort interface {
	// Create sets up compute resources for an environment.
	Create(ctx context.Context, envName string, branch string) (*ComputeResources, error)

	// Start starts per-environment services (infra containers, etc).
	Start(ctx context.Context, envName string, ports PortMap) error

	// Stop stops services without destroying data or resources.
	Stop(ctx context.Context, envName string) error

	// Destroy tears down all compute resources.
	Destroy(ctx context.Context, envName string) error

	// IsRunning checks if environment compute resources are active.
	IsRunning(ctx context.Context, envName string) (bool, error)
}

// NetworkingPort handles port allocation and service URL resolution.
// Local: deterministic offset from base ports.
// Preview: reverse proxy with subdomain routing (future).
type NetworkingPort interface {
	// AllocatePorts returns deterministic port assignments for all services and infrastructure.
	AllocatePorts(envName string) (PortMap, error)

	// GetServiceURL returns the URL to reach a named service in the environment.
	GetServiceURL(envName string, service string) (string, error)
}

// EnvPort generates environment configuration files (.env.local, etc).
type EnvPort interface {
	// Generate writes .env.local files for all services in the environment.
	Generate(ctx context.Context, envName string, workdir string, ports PortMap, databases map[string]*DatabaseInfo) error

	// SymlinkSharedEnvFiles symlinks shared .env files from the main worktree.
	SymlinkSharedEnvFiles(ctx context.Context, workdir string) error

	// Cleanup removes generated env files.
	Cleanup(ctx context.Context, workdir string) error
}

// StatePort persists previewctl state.
// File-based for POC; interface accommodates Postgres/etcd later.
type StatePort interface {
	// Load returns the full state.
	Load(ctx context.Context) (*State, error)

	// Save persists the full state.
	Save(ctx context.Context, state *State) error

	// GetEnvironment returns a single environment entry, or nil if not found.
	GetEnvironment(ctx context.Context, name string) (*EnvironmentEntry, error)

	// SetEnvironment creates or updates an environment entry.
	SetEnvironment(ctx context.Context, name string, entry *EnvironmentEntry) error

	// RemoveEnvironment deletes an environment entry.
	RemoveEnvironment(ctx context.Context, name string) error

	// UpdateSnapshot updates snapshot metadata for a named database.
	UpdateSnapshot(ctx context.Context, dbName string, info *SnapshotUpdate) error
}

// ProgressReporter receives lifecycle step events from the manager.
// Inbound adapters implement this to render progress (CLI spinners, SSE, etc).
type ProgressReporter interface {
	OnStep(event StepEvent)
}

// NoopReporter is a ProgressReporter that discards all events.
type NoopReporter struct{}

func (NoopReporter) OnStep(StepEvent) {}

// ProvisionerPort manages VM lifecycle for preview/sandbox modes.
// Not implemented in POC; exists to validate interface design.
type ProvisionerPort interface {
	Provision(ctx context.Context, envName string, spec VMSpec) (*VMInfo, error)
	Deprovision(ctx context.Context, vmID string) error
	Status(ctx context.Context, vmID string) (*VMInfo, error)
}
