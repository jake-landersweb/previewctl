package domain

import (
	"context"
	"fmt"
	"time"
)

// Manager orchestrates environment lifecycle by coordinating all ports.
// It is the single source of truth — all inbound adapters delegate to it.
type Manager struct {
	databases    map[string]DatabasePort
	compute      ComputePort
	networking   NetworkingPort
	envgen       EnvPort
	state        StatePort
	progress     ProgressReporter
	hooks        *HookRunner
	config       *ProjectConfig
	seedResolver *SeedResolver
	projectRoot  string
}

// ManagerDeps holds the dependencies for creating a Manager.
type ManagerDeps struct {
	Databases    map[string]DatabasePort
	Compute      ComputePort
	Networking   NetworkingPort
	EnvGen       EnvPort
	State        StatePort
	Progress     ProgressReporter
	Config       *ProjectConfig
	ProjectRoot  string
	SeedResolver *SeedResolver
}

// NewManager creates a new Manager with the given dependencies.
func NewManager(deps ManagerDeps) *Manager {
	progress := deps.Progress
	if progress == nil {
		progress = NoopReporter{}
	}
	return &Manager{
		databases:    deps.Databases,
		compute:      deps.Compute,
		networking:   deps.Networking,
		envgen:       deps.EnvGen,
		state:        deps.State,
		progress:     progress,
		hooks:        NewHookRunner(deps.Config.Hooks, progress),
		config:       deps.Config,
		seedResolver: deps.SeedResolver,
		projectRoot:  deps.ProjectRoot,
	}
}

// step executes a lifecycle step with progress reporting and before/after hooks.
// completeMsg is a pointer so the closure can update it dynamically.
func (m *Manager) step(ctx context.Context, name string, startMsg string, completeMsg *string, hctx *HookContext, fn func() error) error {
	// Before hook
	if err := m.hooks.RunBefore(ctx, name, hctx); err != nil {
		return err
	}

	// Execute step
	m.progress.OnStep(StepEvent{Step: name, Status: StepStarted, Message: startMsg})
	if err := fn(); err != nil {
		m.progress.OnStep(StepEvent{Step: name, Status: StepFailed, Error: err})
		return err
	}
	m.progress.OnStep(StepEvent{Step: name, Status: StepCompleted, Message: *completeMsg})

	// After hook
	if err := m.hooks.RunAfter(ctx, name, hctx); err != nil {
		return err
	}

	return nil
}

// msg is a convenience for creating a string pointer for step().
func msg(s string) *string { return &s }

// Init creates a new environment end-to-end.
func (m *Manager) Init(ctx context.Context, envName string, branch string) (*EnvironmentEntry, error) {
	hctx := &HookContext{
		EnvName:     envName,
		Branch:      branch,
		ProjectName: m.config.Name,
	}

	// Lifecycle-level before hook
	if err := m.hooks.RunBefore(ctx, "create", hctx); err != nil {
		return nil, err
	}

	// 1. Allocate ports
	var ports PortMap
	if err := m.step(ctx, "allocate_ports", "Allocating ports...", msg("Ports allocated"), hctx, func() error {
		ports = m.networking.AllocatePorts(envName)
		hctx.Ports = ports
		return nil
	}); err != nil {
		return nil, fmt.Errorf("allocating ports: %w", err)
	}

	// 2. Create compute resources
	var resources *ComputeResources
	if err := m.step(ctx, "create_compute", "Creating compute resources...", msg("Compute resources created"), hctx, func() error {
		var err error
		resources, err = m.compute.Create(ctx, envName, branch)
		if err != nil {
			return err
		}
		hctx.WorktreePath = resources.WorktreePath
		return nil
	}); err != nil {
		return nil, fmt.Errorf("creating compute resources: %w", err)
	}

	// 3. Set up databases
	dbInfos := make(map[string]*DatabaseInfo)
	dbRefs := make(map[string]DatabaseRef)
	for dbName, dbPort := range m.databases {
		if err := m.step(ctx, fmt.Sprintf("ensure_database_%s", dbName),
			fmt.Sprintf("Connecting to %s server...", dbName),
			msg(fmt.Sprintf("Connected to %s server", dbName)),
			hctx, func() error {
				return dbPort.EnsureInfrastructure(ctx)
			}); err != nil {
			return nil, fmt.Errorf("ensuring database %s infrastructure: %w", dbName, err)
		}

		cloneMsg := fmt.Sprintf("Cloned %s database", dbName)
		if err := m.step(ctx, fmt.Sprintf("clone_database_%s", dbName),
			fmt.Sprintf("Cloning %s database from template...", dbName),
			&cloneMsg,
			hctx, func() error {
				dbInfo, err := dbPort.CreateDatabase(ctx, envName)
				if err != nil {
					return err
				}
				dbInfos[dbName] = dbInfo
				dbRefs[dbName] = DatabaseRef{Name: dbInfo.Database, Provider: "local-docker"}
				hctx.Databases = dbInfos
				cloneMsg = fmt.Sprintf("Cloned %s → %s", dbName, dbInfo.Database)
				return nil
			}); err != nil {
			return nil, fmt.Errorf("creating database %s: %w", dbName, err)
		}
	}

	// 4. Symlink shared env files
	if err := m.step(ctx, "symlink_env", "Symlinking shared env files...", msg("Shared env files symlinked"), hctx, func() error {
		return m.envgen.SymlinkSharedEnvFiles(ctx, resources.WorktreePath)
	}); err != nil {
		return nil, fmt.Errorf("symlinking env files: %w", err)
	}

	// 5. Generate env files
	if err := m.step(ctx, "generate_env", "Generating .env.local files...", msg(".env.local files generated"), hctx, func() error {
		return m.envgen.Generate(ctx, envName, resources.WorktreePath, ports, dbInfos)
	}); err != nil {
		return nil, fmt.Errorf("generating env files: %w", err)
	}

	// 6. Start per-env infrastructure
	if err := m.step(ctx, "start_infra", "Starting infrastructure containers...", msg("Infrastructure containers started"), hctx, func() error {
		return m.compute.Start(ctx, envName, ports)
	}); err != nil {
		return nil, fmt.Errorf("starting infrastructure: %w", err)
	}

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

	if err := m.step(ctx, "save_state", "Saving state...", msg("State saved"), hctx, func() error {
		return m.state.SetEnvironment(ctx, envName, entry)
	}); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}

	// Lifecycle-level after hook
	if err := m.hooks.RunAfter(ctx, "create", hctx); err != nil {
		return nil, err
	}

	return entry, nil
}

// Destroy tears down an environment and cleans up all resources.
func (m *Manager) Destroy(ctx context.Context, envName string) error {
	hctx := &HookContext{
		EnvName:     envName,
		ProjectName: m.config.Name,
	}

	// Load state first (no hooks on this — we need the data)
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
	m.progress.OnStep(StepEvent{Step: "load_state", Status: StepCompleted, Message: "Environment state loaded"})

	// Populate hook context from loaded state
	hctx.Branch = entry.Branch
	hctx.Ports = entry.Ports
	if entry.Local != nil {
		hctx.WorktreePath = entry.Local.WorktreePath
	}

	// Lifecycle-level before hook
	if err := m.hooks.RunBefore(ctx, "delete", hctx); err != nil {
		return err
	}

	if err := m.step(ctx, "destroy_compute", "Removing worktree and stopping containers...", msg("Worktree and containers removed"), hctx, func() error {
		return m.compute.Destroy(ctx, envName)
	}); err != nil {
		return fmt.Errorf("destroying compute resources: %w", err)
	}

	for dbName, dbPort := range m.databases {
		if err := m.step(ctx, fmt.Sprintf("destroy_database_%s", dbName),
			fmt.Sprintf("Dropping %s database...", dbName),
			msg(fmt.Sprintf("Database %s dropped", dbName)),
			hctx, func() error {
				return dbPort.DestroyDatabase(ctx, envName)
			}); err != nil {
			return fmt.Errorf("destroying database %s: %w", dbName, err)
		}
	}

	if entry.Local != nil && entry.Local.WorktreePath != "" {
		if err := m.step(ctx, "cleanup_env", "Cleaning up env files...", msg("Env files cleaned up"), hctx, func() error {
			return m.envgen.Cleanup(ctx, entry.Local.WorktreePath)
		}); err != nil {
			return fmt.Errorf("cleaning up env files: %w", err)
		}
	}

	if err := m.step(ctx, "remove_state", "Removing state...", msg("State removed"), hctx, func() error {
		return m.state.RemoveEnvironment(ctx, envName)
	}); err != nil {
		return fmt.Errorf("removing state: %w", err)
	}

	// Lifecycle-level after hook
	if err := m.hooks.RunAfter(ctx, "delete", hctx); err != nil {
		return err
	}

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

	dbCfg, ok := m.config.Core.Databases[dbName]
	if !ok {
		return fmt.Errorf("no config for database '%s'", dbName)
	}

	hctx := &HookContext{
		EnvName:     envName,
		ProjectName: m.config.Name,
	}

	// Lifecycle-level before hook
	if err := m.hooks.RunBefore(ctx, "reset", hctx); err != nil {
		return err
	}

	// Step 1: Drop existing database
	dropMsg := fmt.Sprintf("Dropped database (%s)", dbName)
	if err := m.step(ctx, "drop_database",
		fmt.Sprintf("Dropping database (%s)...", dbName),
		&dropMsg,
		hctx, func() error {
			return dbPort.DestroyDatabase(ctx, envName)
		}); err != nil {
		return fmt.Errorf("dropping database %s: %w", dbName, err)
	}

	// Step 2: Clone from template
	templateDb := ""
	if dbCfg.Local != nil {
		templateDb = dbCfg.Local.TemplateDb
	}
	cloneMsg := fmt.Sprintf("Cloning (%s) from %s...", dbName, templateDb)
	if err := m.step(ctx, "clone_database",
		cloneMsg,
		&cloneMsg,
		hctx, func() error {
			info, err := dbPort.CreateDatabase(ctx, envName)
			if err != nil {
				return err
			}
			hctx.Databases = map[string]*DatabaseInfo{dbName: info}
			cloneMsg = fmt.Sprintf("Cloned (%s) %s → %s", dbName, templateDb, info.Database)
			return nil
		}); err != nil {
		return fmt.Errorf("cloning database %s: %w", dbName, err)
	}

	// Lifecycle-level after hook
	if err := m.hooks.RunAfter(ctx, "reset", hctx); err != nil {
		return err
	}

	return nil
}

// SeedTemplate seeds or refreshes a template database.
func (m *Manager) SeedTemplate(ctx context.Context, dbName string) error {
	dbPort, ok := m.databases[dbName]
	if !ok {
		return fmt.Errorf("unknown database '%s'", dbName)
	}

	dbCfg, ok := m.config.Core.Databases[dbName]
	if !ok {
		return fmt.Errorf("no config for database '%s'", dbName)
	}

	modeCfg := dbCfg.Local
	if modeCfg == nil {
		return fmt.Errorf("database '%s' has no local config", dbName)
	}

	hctx := &HookContext{
		EnvName:     "",
		ProjectName: m.config.Name,
	}

	// Lifecycle-level before hook
	if err := m.hooks.RunBefore(ctx, "seed", hctx); err != nil {
		return err
	}

	connectMsg := fmt.Sprintf("Connected to %s:%d", dbCfg.Engine, modeCfg.Port)
	if err := m.step(ctx, "ensure_infra",
		fmt.Sprintf("Connecting to %s on port %d...", dbCfg.Engine, modeCfg.Port),
		&connectMsg,
		hctx, func() error {
			return dbPort.EnsureInfrastructure(ctx)
		}); err != nil {
		return fmt.Errorf("ensuring infrastructure: %w", err)
	}

	// Resolve seed steps
	var materials []*SeedMaterial
	if len(modeCfg.Seed) > 0 && m.seedResolver != nil {
		resolveMsg := fmt.Sprintf("Resolved %d seed step(s)", len(modeCfg.Seed))
		if err := m.step(ctx, "resolve_seed",
			fmt.Sprintf("Resolving %d seed step(s)...", len(modeCfg.Seed)),
			&resolveMsg,
			hctx, func() error {
				var err error
				materials, err = m.seedResolver.Resolve(ctx, modeCfg.Seed, m.projectRoot)
				return err
			}); err != nil {
			return fmt.Errorf("resolving seed: %w", err)
		}
	}

	seedSource := fmt.Sprintf("%d step(s)", len(modeCfg.Seed))
	if len(modeCfg.Seed) == 0 {
		seedSource = "empty template"
	}
	seedCompleteMsg := fmt.Sprintf("Seeded %s from %s", modeCfg.TemplateDb, seedSource)
	if err := m.step(ctx, "seed_template",
		fmt.Sprintf("Seeding %s from %s...", modeCfg.TemplateDb, seedSource),
		&seedCompleteMsg,
		hctx, func() error {
			return dbPort.SeedTemplate(ctx, materials)
		}); err != nil {
		return fmt.Errorf("seeding template: %w", err)
	}

	now := time.Now()
	ready := true
	if err := m.state.UpdateSnapshot(ctx, dbName, &SnapshotUpdate{
		LastSeeded:    &now,
		TemplateReady: &ready,
	}); err != nil {
		return fmt.Errorf("updating snapshot state: %w", err)
	}

	// Lifecycle-level after hook
	if err := m.hooks.RunAfter(ctx, "seed", hctx); err != nil {
		return err
	}

	return nil
}
