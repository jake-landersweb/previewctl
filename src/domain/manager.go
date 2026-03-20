package domain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Manager orchestrates environment lifecycle by coordinating all ports.
// It is the single source of truth — all inbound adapters delegate to it.
type Manager struct {
	compute     ComputePort
	networking  NetworkingPort
	envgen      EnvPort
	state       StatePort
	progress    ProgressReporter
	hooks       *HookRunner
	config      *ProjectConfig
	projectRoot string
}

// ManagerDeps holds the dependencies for creating a Manager.
type ManagerDeps struct {
	Compute     ComputePort
	Networking  NetworkingPort
	EnvGen      EnvPort
	State       StatePort
	Progress    ProgressReporter
	Config      *ProjectConfig
	ProjectRoot string
}

// NewManager creates a new Manager with the given dependencies.
func NewManager(deps ManagerDeps) *Manager {
	progress := deps.Progress
	if progress == nil {
		progress = NoopReporter{}
	}
	return &Manager{
		compute:     deps.Compute,
		networking:  deps.Networking,
		envgen:      deps.EnvGen,
		state:       deps.State,
		progress:    progress,
		hooks:       NewHookRunner(deps.Config.Hooks, progress),
		config:      deps.Config,
		projectRoot: deps.ProjectRoot,
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
		var err error
		ports, err = m.networking.AllocatePorts(envName)
		if err != nil {
			return err
		}
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

	// 3. Run core service "seed" hooks
	coreOutputs := make(map[string]map[string]string)
	for svcName, svc := range m.config.Core.Services {
		if svc.Hooks == nil || svc.Hooks.Seed == "" {
			continue
		}
		seedMsg := fmt.Sprintf("Seeded core service %s", svcName)
		if err := m.step(ctx, fmt.Sprintf("seed_core_%s", svcName),
			fmt.Sprintf("Running seed hook for %s...", svcName),
			&seedMsg,
			hctx, func() error {
				m.progress.OnStep(StepEvent{Step: fmt.Sprintf("seed_core_%s", svcName), Status: StepStreaming})
				outputs, err := m.runCoreHook(ctx, svcName, "seed", envName, ports)
				if err != nil {
					return err
				}
				coreOutputs[svcName] = outputs
				hctx.CoreOutputs = coreOutputs
				return nil
			}); err != nil {
			return nil, fmt.Errorf("seeding core service %s: %w", svcName, err)
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
		return m.envgen.Generate(ctx, envName, resources.WorktreePath, ports, coreOutputs)
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
		Name:        envName,
		Mode:        ModeLocal,
		Branch:      branch,
		Status:      StatusRunning,
		CreatedAt:   now,
		UpdatedAt:   now,
		Ports:       ports,
		CoreOutputs: coreOutputs,
		Local: &LocalMeta{
			WorktreePath:       resources.WorktreePath,
			ComposeProjectName: fmt.Sprintf("%s-%s", m.config.Name, envName),
			ManagedWorktree:    true,
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

// Attach creates a preview environment using an existing worktree.
// It runs everything except worktree creation: ports, core services, env files, infra.
func (m *Manager) Attach(ctx context.Context, envName string, worktreePath string) (*EnvironmentEntry, error) {
	// Detect branch from the worktree
	branch := detectBranch(worktreePath)

	hctx := &HookContext{
		EnvName:      envName,
		Branch:       branch,
		ProjectName:  m.config.Name,
		WorktreePath: worktreePath,
	}

	if err := m.hooks.RunBefore(ctx, "create", hctx); err != nil {
		return nil, err
	}

	// 1. Allocate ports
	var ports PortMap
	if err := m.step(ctx, "allocate_ports", "Allocating ports...", msg("Ports allocated"), hctx, func() error {
		var err error
		ports, err = m.networking.AllocatePorts(envName)
		if err != nil {
			return err
		}
		hctx.Ports = ports
		return nil
	}); err != nil {
		return nil, fmt.Errorf("allocating ports: %w", err)
	}

	// 2. Run core service "seed" hooks
	coreOutputs := make(map[string]map[string]string)
	for svcName, svc := range m.config.Core.Services {
		if svc.Hooks == nil || svc.Hooks.Seed == "" {
			continue
		}
		seedMsg := fmt.Sprintf("Seeded core service %s", svcName)
		if err := m.step(ctx, fmt.Sprintf("seed_core_%s", svcName),
			fmt.Sprintf("Running seed hook for %s...", svcName),
			&seedMsg,
			hctx, func() error {
				m.progress.OnStep(StepEvent{Step: fmt.Sprintf("seed_core_%s", svcName), Status: StepStreaming})
				outputs, err := m.runCoreHook(ctx, svcName, "seed", envName, ports)
				if err != nil {
					return err
				}
				coreOutputs[svcName] = outputs
				hctx.CoreOutputs = coreOutputs
				return nil
			}); err != nil {
			return nil, fmt.Errorf("seeding core service %s: %w", svcName, err)
		}
	}

	// 3. Symlink shared env files
	if err := m.step(ctx, "symlink_env", "Symlinking shared env files...", msg("Shared env files symlinked"), hctx, func() error {
		return m.envgen.SymlinkSharedEnvFiles(ctx, worktreePath)
	}); err != nil {
		return nil, fmt.Errorf("symlinking env files: %w", err)
	}

	// 4. Generate env files
	if err := m.step(ctx, "generate_env", "Generating .env.local files...", msg(".env.local files generated"), hctx, func() error {
		return m.envgen.Generate(ctx, envName, worktreePath, ports, coreOutputs)
	}); err != nil {
		return nil, fmt.Errorf("generating env files: %w", err)
	}

	// 5. Start per-env infrastructure
	if err := m.step(ctx, "start_infra", "Starting infrastructure containers...", msg("Infrastructure containers started"), hctx, func() error {
		return m.compute.Start(ctx, envName, ports)
	}); err != nil {
		return nil, fmt.Errorf("starting infrastructure: %w", err)
	}

	// 6. Persist state
	now := time.Now()
	entry := &EnvironmentEntry{
		Name:        envName,
		Mode:        ModeLocal,
		Branch:      branch,
		Status:      StatusRunning,
		CreatedAt:   now,
		UpdatedAt:   now,
		Ports:       ports,
		CoreOutputs: coreOutputs,
		Local: &LocalMeta{
			WorktreePath:       worktreePath,
			ComposeProjectName: fmt.Sprintf("%s-%s", m.config.Name, envName),
			ManagedWorktree:    false,
		},
	}

	if err := m.step(ctx, "save_state", "Saving state...", msg("State saved"), hctx, func() error {
		return m.state.SetEnvironment(ctx, envName, entry)
	}); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}

	if err := m.hooks.RunAfter(ctx, "create", hctx); err != nil {
		return nil, err
	}

	return entry, nil
}

// detectBranch tries to determine the git branch of a worktree path.
func detectBranch(worktreePath string) string {
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
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

	// Run core service "destroy" hooks before removing the worktree
	for svcName, svc := range m.config.Core.Services {
		if svc.Hooks == nil || svc.Hooks.Destroy == "" {
			continue
		}
		destroyMsg := fmt.Sprintf("Destroyed core service %s", svcName)
		if err := m.step(ctx, fmt.Sprintf("destroy_core_%s", svcName),
			fmt.Sprintf("Running destroy hook for %s...", svcName),
			&destroyMsg,
			hctx, func() error {
				m.progress.OnStep(StepEvent{Step: fmt.Sprintf("destroy_core_%s", svcName), Status: StepStreaming})
				_, err := m.runCoreHook(ctx, svcName, "destroy", envName, entry.Ports)
				return err
			}); err != nil {
			return fmt.Errorf("destroying core service %s: %w", svcName, err)
		}
	}

	if entry.Local != nil && entry.Local.ManagedWorktree {
		if err := m.step(ctx, "destroy_compute", "Removing worktree and stopping containers...", msg("Worktree and containers removed"), hctx, func() error {
			return m.compute.Destroy(ctx, envName)
		}); err != nil {
			return fmt.Errorf("destroying compute resources: %w", err)
		}
	} else {
		// Attached worktree — only stop containers, don't remove the worktree
		if err := m.step(ctx, "stop_infra", "Stopping infrastructure containers...", msg("Infrastructure containers stopped"), hctx, func() error {
			return m.compute.Stop(ctx, envName)
		}); err != nil {
			return fmt.Errorf("stopping infrastructure: %w", err)
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

	return &EnvironmentDetail{
		Entry:        entry,
		InfraRunning: infraRunning,
	}, nil
}

// RunCoreHook runs a core service hook for a given environment, loading ports from state.
func (m *Manager) RunCoreHook(ctx context.Context, svcName, action, envName string) (map[string]string, error) {
	entry, err := m.state.GetEnvironment(ctx, envName)
	if err != nil {
		return nil, fmt.Errorf("loading environment: %w", err)
	}
	var ports PortMap
	if entry != nil {
		ports = entry.Ports
	}
	return m.runCoreHook(ctx, svcName, action, envName, ports)
}

// runCoreHook executes a core service hook with proper env vars and returns captured outputs.
func (m *Manager) runCoreHook(ctx context.Context, svcName, action, envName string, ports PortMap) (map[string]string, error) {
	svc, ok := m.config.Core.Services[svcName]
	if !ok {
		return nil, fmt.Errorf("unknown core service '%s'", svcName)
	}

	var hookScript string
	if svc.Hooks != nil {
		switch action {
		case "init":
			hookScript = svc.Hooks.Init
		case "seed":
			hookScript = svc.Hooks.Seed
		case "reset":
			hookScript = svc.Hooks.Reset
		case "destroy":
			hookScript = svc.Hooks.Destroy
		}
	}
	if hookScript == "" {
		return nil, nil
	}

	// Build environment variables
	env := append(os.Environ(),
		fmt.Sprintf("PREVIEWCTL_ENV_NAME=%s", envName),
		fmt.Sprintf("PREVIEWCTL_ACTION=%s", action),
		fmt.Sprintf("PREVIEWCTL_PROJECT_NAME=%s", m.config.Name),
		fmt.Sprintf("PREVIEWCTL_SERVICE_NAME=%s", svcName),
		fmt.Sprintf("PREVIEWCTL_PROJECT_ROOT=%s", m.projectRoot),
	)

	// Add port env vars
	for name, port := range ports {
		envVar := fmt.Sprintf("PREVIEWCTL_PORT_%s=%d",
			strings.ToUpper(strings.ReplaceAll(name, "-", "_")), port)
		env = append(env, envVar)
	}

	// Only validate outputs for actions that produce state (seed, reset)
	var requiredOutputs []string
	if action == "seed" || action == "reset" {
		requiredOutputs = svc.Outputs
	}

	return ExecuteCoreHook(ctx, hookScript, requiredOutputs, env, m.projectRoot)
}

// CoreInit runs the "init" hook for a core service (one-time setup).
func (m *Manager) CoreInit(ctx context.Context, svcName string) error {
	svc, ok := m.config.Core.Services[svcName]
	if !ok {
		return fmt.Errorf("unknown core service '%s'", svcName)
	}
	if svc.Hooks == nil || svc.Hooks.Init == "" {
		return fmt.Errorf("core service '%s' has no init hook defined", svcName)
	}

	hctx := &HookContext{
		ProjectName: m.config.Name,
		ProjectRoot: m.projectRoot,
	}

	initMsg := fmt.Sprintf("Initialized core service %s", svcName)
	if err := m.step(ctx, "core_init",
		fmt.Sprintf("Running init hook for %s...", svcName),
		&initMsg,
		hctx, func() error {
			m.progress.OnStep(StepEvent{Step: "core_init", Status: StepStreaming})
			_, err := m.runCoreHook(ctx, svcName, "init", "", nil)
			return err
		}); err != nil {
		return fmt.Errorf("initializing core service %s: %w", svcName, err)
	}

	return nil
}

// CoreReset runs the "reset" hook for a core service on a specific environment.
func (m *Manager) CoreReset(ctx context.Context, svcName, envName string) error {
	svc, ok := m.config.Core.Services[svcName]
	if !ok {
		return fmt.Errorf("unknown core service '%s'", svcName)
	}
	if svc.Hooks == nil || svc.Hooks.Reset == "" {
		return fmt.Errorf("core service '%s' has no reset hook defined", svcName)
	}

	entry, err := m.state.GetEnvironment(ctx, envName)
	if err != nil {
		return fmt.Errorf("loading environment: %w", err)
	}
	if entry == nil {
		return fmt.Errorf("environment '%s' not found", envName)
	}

	hctx := &HookContext{
		EnvName:     envName,
		ProjectName: m.config.Name,
		ProjectRoot: m.projectRoot,
		Ports:       entry.Ports,
		CoreOutputs: entry.CoreOutputs,
	}

	resetMsg := fmt.Sprintf("Reset core service %s for %s", svcName, envName)
	if err := m.step(ctx, "core_reset",
		fmt.Sprintf("Running reset hook for %s...", svcName),
		&resetMsg,
		hctx, func() error {
			m.progress.OnStep(StepEvent{Step: "core_reset", Status: StepStreaming})
			outputs, err := m.runCoreHook(ctx, svcName, "reset", envName, entry.Ports)
			if err != nil {
				return err
			}
			// Update stored outputs if hook produced new ones
			if outputs != nil {
				if entry.CoreOutputs == nil {
					entry.CoreOutputs = make(map[string]map[string]string)
				}
				entry.CoreOutputs[svcName] = outputs
				return m.state.SetEnvironment(ctx, envName, entry)
			}
			return nil
		}); err != nil {
		return fmt.Errorf("resetting core service %s: %w", svcName, err)
	}

	return nil
}
