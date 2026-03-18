package domain

import (
	"context"
	"fmt"
	"time"
)

// Manager orchestrates environment lifecycle by coordinating all ports.
// It is the single source of truth — all inbound adapters delegate to it.
type Manager struct {
	databases  map[string]DatabasePort
	compute    ComputePort
	networking NetworkingPort
	envgen     EnvPort
	state      StatePort
	progress   ProgressReporter
	config     *ProjectConfig
}

// ManagerDeps holds the dependencies for creating a Manager.
type ManagerDeps struct {
	Databases  map[string]DatabasePort
	Compute    ComputePort
	Networking NetworkingPort
	EnvGen     EnvPort
	State      StatePort
	Progress   ProgressReporter
	Config     *ProjectConfig
}

// NewManager creates a new Manager with the given dependencies.
func NewManager(deps ManagerDeps) *Manager {
	progress := deps.Progress
	if progress == nil {
		progress = NoopReporter{}
	}
	return &Manager{
		databases:  deps.Databases,
		compute:    deps.Compute,
		networking: deps.Networking,
		envgen:     deps.EnvGen,
		state:      deps.State,
		progress:   progress,
		config:     deps.Config,
	}
}

// Init creates a new environment end-to-end.
func (m *Manager) Init(ctx context.Context, envName string, branch string) (*EnvironmentEntry, error) {
	// 1. Allocate ports
	m.progress.OnStep(StepEvent{Step: "allocate_ports", Status: StepStarted, Message: "Allocating ports..."})
	ports := m.networking.AllocatePorts(envName)
	m.progress.OnStep(StepEvent{Step: "allocate_ports", Status: StepCompleted, Message: "Ports allocated"})

	// 2. Create compute resources
	m.progress.OnStep(StepEvent{Step: "create_compute", Status: StepStarted, Message: "Creating compute resources..."})
	resources, err := m.compute.Create(ctx, envName, branch)
	if err != nil {
		m.progress.OnStep(StepEvent{Step: "create_compute", Status: StepFailed, Error: err})
		return nil, fmt.Errorf("creating compute resources: %w", err)
	}
	m.progress.OnStep(StepEvent{Step: "create_compute", Status: StepCompleted, Message: "Compute resources created"})

	// 3. Set up databases
	dbInfos := make(map[string]*DatabaseInfo)
	dbRefs := make(map[string]DatabaseRef)
	for dbName, dbPort := range m.databases {
		stepName := fmt.Sprintf("ensure_database_%s", dbName)
		m.progress.OnStep(StepEvent{Step: stepName, Status: StepStarted, Message: fmt.Sprintf("Connecting to %s server...", dbName)})
		if err := dbPort.EnsureInfrastructure(ctx); err != nil {
			m.progress.OnStep(StepEvent{Step: stepName, Status: StepFailed, Error: err})
			return nil, fmt.Errorf("ensuring database %s infrastructure: %w", dbName, err)
		}
		m.progress.OnStep(StepEvent{Step: stepName, Status: StepCompleted, Message: fmt.Sprintf("Connected to %s server", dbName)})

		stepName = fmt.Sprintf("clone_database_%s", dbName)
		m.progress.OnStep(StepEvent{Step: stepName, Status: StepStarted, Message: fmt.Sprintf("Cloning %s database from template...", dbName)})
		dbInfo, err := dbPort.CreateDatabase(ctx, envName)
		if err != nil {
			m.progress.OnStep(StepEvent{Step: stepName, Status: StepFailed, Error: err})
			return nil, fmt.Errorf("creating database %s: %w", dbName, err)
		}
		m.progress.OnStep(StepEvent{Step: stepName, Status: StepCompleted, Message: fmt.Sprintf("Cloned %s → %s", dbName, dbInfo.Database)})

		dbInfos[dbName] = dbInfo
		dbRefs[dbName] = DatabaseRef{Name: dbInfo.Database, Provider: "local-docker"}
	}

	// 4. Symlink shared env files
	m.progress.OnStep(StepEvent{Step: "symlink_env", Status: StepStarted, Message: "Symlinking shared env files..."})
	if err := m.envgen.SymlinkSharedEnvFiles(ctx, resources.WorktreePath); err != nil {
		m.progress.OnStep(StepEvent{Step: "symlink_env", Status: StepFailed, Error: err})
		return nil, fmt.Errorf("symlinking env files: %w", err)
	}
	m.progress.OnStep(StepEvent{Step: "symlink_env", Status: StepCompleted})

	// 5. Generate env files
	m.progress.OnStep(StepEvent{Step: "generate_env", Status: StepStarted, Message: "Generating environment files..."})
	if err := m.envgen.Generate(ctx, envName, resources.WorktreePath, ports, dbInfos); err != nil {
		m.progress.OnStep(StepEvent{Step: "generate_env", Status: StepFailed, Error: err})
		return nil, fmt.Errorf("generating env files: %w", err)
	}
	m.progress.OnStep(StepEvent{Step: "generate_env", Status: StepCompleted})

	// 6. Start per-env infrastructure
	m.progress.OnStep(StepEvent{Step: "start_infra", Status: StepStarted, Message: "Starting infrastructure..."})
	if err := m.compute.Start(ctx, envName, ports); err != nil {
		m.progress.OnStep(StepEvent{Step: "start_infra", Status: StepFailed, Error: err})
		return nil, fmt.Errorf("starting infrastructure: %w", err)
	}
	m.progress.OnStep(StepEvent{Step: "start_infra", Status: StepCompleted})

	// 7. Persist state
	now := time.Now()
	entry := &EnvironmentEntry{
		Name:      envName,
		Mode:      ModeLocal,
		Branch:    branch,
		Status:    StatusRunning,
		CreatedAt: now,
		UpdatedAt: now,
		Ports:     ports,
		Databases: dbRefs,
		Local: &LocalMeta{
			WorktreePath:       resources.WorktreePath,
			ComposeProjectName: fmt.Sprintf("%s-%s", m.config.Name, envName),
		},
	}

	m.progress.OnStep(StepEvent{Step: "save_state", Status: StepStarted, Message: "Saving state..."})
	if err := m.state.SetEnvironment(ctx, envName, entry); err != nil {
		m.progress.OnStep(StepEvent{Step: "save_state", Status: StepFailed, Error: err})
		return nil, fmt.Errorf("saving state: %w", err)
	}
	m.progress.OnStep(StepEvent{Step: "save_state", Status: StepCompleted})

	return entry, nil
}

// Destroy tears down an environment and cleans up all resources.
func (m *Manager) Destroy(ctx context.Context, envName string) error {
	m.progress.OnStep(StepEvent{Step: "load_state", Status: StepStarted, Message: "Loading environment state..."})
	entry, err := m.state.GetEnvironment(ctx, envName)
	if err != nil {
		m.progress.OnStep(StepEvent{Step: "load_state", Status: StepFailed, Error: err})
		return fmt.Errorf("loading environment: %w", err)
	}
	if entry == nil {
		err := fmt.Errorf("environment '%s' not found", envName)
		m.progress.OnStep(StepEvent{Step: "load_state", Status: StepFailed, Error: err})
		return err
	}
	m.progress.OnStep(StepEvent{Step: "load_state", Status: StepCompleted})

	m.progress.OnStep(StepEvent{Step: "destroy_compute", Status: StepStarted, Message: "Destroying compute resources..."})
	if err := m.compute.Destroy(ctx, envName); err != nil {
		m.progress.OnStep(StepEvent{Step: "destroy_compute", Status: StepFailed, Error: err})
		return fmt.Errorf("destroying compute resources: %w", err)
	}
	m.progress.OnStep(StepEvent{Step: "destroy_compute", Status: StepCompleted})

	for dbName, dbPort := range m.databases {
		stepName := fmt.Sprintf("destroy_database_%s", dbName)
		m.progress.OnStep(StepEvent{Step: stepName, Status: StepStarted, Message: fmt.Sprintf("Destroying %s database...", dbName)})
		if err := dbPort.DestroyDatabase(ctx, envName); err != nil {
			m.progress.OnStep(StepEvent{Step: stepName, Status: StepFailed, Error: err})
			return fmt.Errorf("destroying database %s: %w", dbName, err)
		}
		m.progress.OnStep(StepEvent{Step: stepName, Status: StepCompleted})
	}

	if entry.Local != nil && entry.Local.WorktreePath != "" {
		m.progress.OnStep(StepEvent{Step: "cleanup_env", Status: StepStarted, Message: "Cleaning up env files..."})
		if err := m.envgen.Cleanup(ctx, entry.Local.WorktreePath); err != nil {
			m.progress.OnStep(StepEvent{Step: "cleanup_env", Status: StepFailed, Error: err})
			return fmt.Errorf("cleaning up env files: %w", err)
		}
		m.progress.OnStep(StepEvent{Step: "cleanup_env", Status: StepCompleted})
	}

	m.progress.OnStep(StepEvent{Step: "remove_state", Status: StepStarted, Message: "Removing state..."})
	if err := m.state.RemoveEnvironment(ctx, envName); err != nil {
		m.progress.OnStep(StepEvent{Step: "remove_state", Status: StepFailed, Error: err})
		return fmt.Errorf("removing state: %w", err)
	}
	m.progress.OnStep(StepEvent{Step: "remove_state", Status: StepCompleted})

	return nil
}

// List returns all tracked environments.
func (m *Manager) List(ctx context.Context) ([]*EnvironmentEntry, error) {
	state, err := m.state.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading state: %w", err)
	}
	entries := make([]*EnvironmentEntry, 0, len(state.Environments))
	for _, entry := range state.Environments {
		entries = append(entries, entry)
	}
	return entries, nil
}

// Status returns detailed status of an environment with live infrastructure checks.
func (m *Manager) Status(ctx context.Context, envName string) (*EnvironmentDetail, error) {
	entry, err := m.state.GetEnvironment(ctx, envName)
	if err != nil {
		return nil, fmt.Errorf("loading environment: %w", err)
	}
	if entry == nil {
		return nil, fmt.Errorf("environment '%s' not found", envName)
	}

	infraRunning, err := m.compute.IsRunning(ctx, envName)
	if err != nil {
		return nil, fmt.Errorf("checking infra status: %w", err)
	}

	dbExists := make(map[string]bool)
	for dbName, dbPort := range m.databases {
		exists, err := dbPort.DatabaseExists(ctx, envName)
		if err != nil {
			return nil, fmt.Errorf("checking database %s: %w", dbName, err)
		}
		dbExists[dbName] = exists
	}

	fullState, err := m.state.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading full state: %w", err)
	}

	return &EnvironmentDetail{
		Entry:          entry,
		InfraRunning:   infraRunning,
		DatabaseExists: dbExists,
		SnapshotInfo:   fullState.Snapshots,
	}, nil
}

// ResetDatabase resets an environment's database from the template.
func (m *Manager) ResetDatabase(ctx context.Context, envName string, dbName string) error {
	dbPort, ok := m.databases[dbName]
	if !ok {
		return fmt.Errorf("unknown database '%s'", dbName)
	}

	m.progress.OnStep(StepEvent{Step: "reset_database", Status: StepStarted, Message: fmt.Sprintf("Resetting %s database...", dbName)})
	_, err := dbPort.ResetDatabase(ctx, envName)
	if err != nil {
		m.progress.OnStep(StepEvent{Step: "reset_database", Status: StepFailed, Error: err})
		return fmt.Errorf("resetting database %s: %w", dbName, err)
	}
	m.progress.OnStep(StepEvent{Step: "reset_database", Status: StepCompleted})
	return nil
}

// SeedTemplate seeds or refreshes a template database.
func (m *Manager) SeedTemplate(ctx context.Context, dbName string, snapshotPath string) error {
	dbPort, ok := m.databases[dbName]
	if !ok {
		return fmt.Errorf("unknown database '%s'", dbName)
	}

	m.progress.OnStep(StepEvent{Step: "ensure_infra", Status: StepStarted, Message: "Ensuring database infrastructure..."})
	if err := dbPort.EnsureInfrastructure(ctx); err != nil {
		m.progress.OnStep(StepEvent{Step: "ensure_infra", Status: StepFailed, Error: err})
		return fmt.Errorf("ensuring infrastructure: %w", err)
	}
	m.progress.OnStep(StepEvent{Step: "ensure_infra", Status: StepCompleted})

	m.progress.OnStep(StepEvent{Step: "seed_template", Status: StepStarted, Message: "Seeding template database..."})
	if err := dbPort.SeedTemplate(ctx, snapshotPath); err != nil {
		m.progress.OnStep(StepEvent{Step: "seed_template", Status: StepFailed, Error: err})
		return fmt.Errorf("seeding template: %w", err)
	}
	m.progress.OnStep(StepEvent{Step: "seed_template", Status: StepCompleted})

	now := time.Now()
	ready := true
	if err := m.state.UpdateSnapshot(ctx, dbName, &SnapshotUpdate{
		LastSeeded:    &now,
		TemplateReady: &ready,
	}); err != nil {
		return fmt.Errorf("updating snapshot state: %w", err)
	}

	return nil
}
