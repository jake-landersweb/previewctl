package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Manager orchestrates environment lifecycle by coordinating all ports.
// It is the single source of truth — all inbound adapters delegate to it.
type Manager struct {
	compute     ComputePort
	networking  NetworkingPort
	state       StatePort
	progress    ProgressReporter
	config      *ProjectConfig
	projectRoot string
}

// ManagerDeps holds the dependencies for creating a Manager.
type ManagerDeps struct {
	Compute     ComputePort
	Networking  NetworkingPort
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
		state:       deps.State,
		progress:    progress,
		config:      deps.Config,
		projectRoot: deps.ProjectRoot,
	}
}

// step executes a lifecycle step with progress reporting.
// completeMsg is a pointer so the closure can update it dynamically.
func (m *Manager) step(_ context.Context, name string, startMsg string, completeMsg *string, fn func() error) error {
	m.progress.OnStep(StepEvent{Step: name, Status: StepStarted, Message: startMsg})
	if err := fn(); err != nil {
		m.progress.OnStep(StepEvent{Step: name, Status: StepFailed, Error: err})
		return err
	}
	m.progress.OnStep(StepEvent{Step: name, Status: StepCompleted, Message: *completeMsg})
	return nil
}

// msg is a convenience for creating a string pointer for step().
func msg(s string) *string { return &s }

// Init creates a new environment end-to-end: creates a worktree, then provisions it.
func (m *Manager) Init(ctx context.Context, envName string, branch string) (*EnvironmentEntry, error) {
	// PROVISIONER PHASE
	ca, manifest, err := m.runProvisioner(ctx, envName, branch, "", true)
	if err != nil {
		return nil, err
	}

	// RUNNER PHASE
	return m.runRunner(ctx, envName, branch, ca, manifest, true)
}

// Attach creates a preview environment using an existing worktree.
// The worktree is not managed by previewctl and will not be removed on delete.
func (m *Manager) Attach(ctx context.Context, envName string, worktreePath string) (*EnvironmentEntry, error) {
	// Detect branch from the worktree
	branch, err := m.compute.DetectBranch(ctx, worktreePath)
	if err != nil {
		return nil, fmt.Errorf("detecting branch: %w", err)
	}

	// Check for duplicate attach
	existing, err := m.state.GetEnvironment(ctx, envName)
	if err != nil {
		return nil, fmt.Errorf("checking existing state: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("environment '%s' already exists — use 'delete' first or choose a different name", envName)
	}

	// PROVISIONER PHASE (skip worktree creation, use existing path)
	ca, manifest, err := m.runProvisioner(ctx, envName, branch, worktreePath, false)
	if err != nil {
		return nil, err
	}

	// RUNNER PHASE
	return m.runRunner(ctx, envName, branch, ca, manifest, false)
}

// runProvisioner executes the provisioner phase:
// 1. provisioner.before hook
// 2. Create compute (worktree for local, hook for remote) — or use existing
// 3. Construct ComputeAccess
// 4. Allocate ports
// 5. Run provisioner service seed hooks
// 6. Build manifest
// 7. Write .previewctl.json via ComputeAccess
func (m *Manager) runProvisioner(ctx context.Context, envName, branch, existingWorktree string, createWorktree bool) (ComputeAccess, *Manifest, error) {
	// 1. Run provisioner.before hook if defined
	if m.config.Provisioner.Before != "" {
		beforeMsg := fmt.Sprintf("Ran provisioner.before (%s)", m.config.Provisioner.Before)
		if err := m.step(ctx, "provisioner_before",
			fmt.Sprintf("Running provisioner.before → %s", m.config.Provisioner.Before),
			&beforeMsg,
			func() error {
				m.progress.OnStep(StepEvent{Step: "provisioner_before", Status: StepStreaming})
				env := m.buildHookEnv(envName, existingWorktree, nil)
				_, err := ExecuteCoreHook(ctx, m.config.Provisioner.Before, nil, env, m.projectRoot)
				return err
			}); err != nil {
			return nil, nil, fmt.Errorf("provisioner before hook: %w", err)
		}
	}

	// 2. Create or reuse compute resources
	var worktreePath string
	if existingWorktree != "" {
		worktreePath = existingWorktree
	} else if createWorktree {
		var resources *ComputeResources
		if err := m.step(ctx, "create_compute", "Creating compute resources...", msg("Compute resources created"), func() error {
			var err error
			resources, err = m.compute.Create(ctx, envName, branch)
			return err
		}); err != nil {
			return nil, nil, fmt.Errorf("creating compute resources: %w", err)
		}
		worktreePath = resources.WorktreePath
	}

	// 3. Construct ComputeAccess
	ca := NewDomainLocalComputeAccess(worktreePath)

	// 4. Allocate ports
	var ports PortMap
	if err := m.step(ctx, "allocate_ports", "Allocating ports...", msg("Ports allocated"), func() error {
		var err error
		ports, err = m.networking.AllocatePorts(envName)
		return err
	}); err != nil {
		return nil, nil, fmt.Errorf("allocating ports: %w", err)
	}

	// 5. Run provisioner service "seed" hooks
	provisionerOutputs := make(map[string]map[string]string)
	for svcName, svc := range m.config.Provisioner.Services {
		if svc.Seed == "" {
			continue
		}
		seedMsg := fmt.Sprintf("Seeded %s (%s)", svcName, svc.Seed)
		if err := m.step(ctx, fmt.Sprintf("seed_%s", svcName),
			fmt.Sprintf("Seeding %s → %s", svcName, svc.Seed),
			&seedMsg,
			func() error {
				m.progress.OnStep(StepEvent{Step: fmt.Sprintf("seed_%s", svcName), Status: StepStreaming})
				outputs, err := m.runCoreHook(ctx, svcName, "seed", envName, ports)
				if err != nil {
					return err
				}
				provisionerOutputs[svcName] = outputs
				return nil
			}); err != nil {
			return nil, nil, fmt.Errorf("seeding %s: %w", svcName, err)
		}
	}

	// 6. Build manifest
	mode := m.config.Mode
	if mode == "" {
		mode = "local"
	}
	var manifest *Manifest
	if err := m.step(ctx, "build_manifest", "Building manifest...", msg("Manifest built"), func() error {
		var err error
		manifest, err = BuildManifest(m.config, envName, branch, mode, ports, provisionerOutputs)
		return err
	}); err != nil {
		return nil, nil, fmt.Errorf("building manifest: %w", err)
	}

	// 7. Write .previewctl.json via ComputeAccess
	if err := m.step(ctx, "write_manifest", "Writing manifest...", msg("Manifest written to .previewctl.json"), func() error {
		data, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		return ca.WriteFile(ctx, ".previewctl.json", data, 0o644)
	}); err != nil {
		return nil, nil, fmt.Errorf("writing manifest: %w", err)
	}

	// 8. Run provisioner.after hook if defined (runs on orchestrator, not via ComputeAccess)
	if m.config.Provisioner.After != "" {
		afterMsg := fmt.Sprintf("Ran provisioner.after (%s)", m.config.Provisioner.After)
		if err := m.step(ctx, "provisioner_after",
			fmt.Sprintf("Running provisioner.after → %s", m.config.Provisioner.After),
			&afterMsg,
			func() error {
				m.progress.OnStep(StepEvent{Step: "provisioner_after", Status: StepStreaming})
				env := m.buildHookEnv(envName, ca.Root(), manifest.Ports)
				_, err := ExecuteCoreHook(ctx, m.config.Provisioner.After, nil, env, m.projectRoot)
				return err
			}); err != nil {
			return nil, nil, fmt.Errorf("provisioner after hook: %w", err)
		}
	}

	return ca, manifest, nil
}

// runRunner executes the runner phase:
// 1. runner.before via ComputeAccess.Exec
// 2. Generate .env files via ComputeAccess.WriteFile
// 3. Start infrastructure via compute port
// 4. runner.deploy via ComputeAccess.Exec
// 5. runner.after via ComputeAccess.Exec
// 6. Save state
func (m *Manager) runRunner(ctx context.Context, envName, branch string, ca ComputeAccess, manifest *Manifest, managedWorktree bool) (*EnvironmentEntry, error) {
	// 1. runner.before hook
	if m.config.Runner != nil && m.config.Runner.Before != "" {
		beforeMsg := fmt.Sprintf("Ran runner.before (%s)", m.config.Runner.Before)
		if err := m.step(ctx, "runner_before",
			fmt.Sprintf("Running runner.before → %s", m.config.Runner.Before),
			&beforeMsg,
			func() error {
				m.progress.OnStep(StepEvent{Step: "runner_before", Status: StepStreaming})
				env := m.buildHookEnv(envName, ca.Root(), manifest.Ports)
				_, err := ca.Exec(ctx, m.config.Runner.Before, env)
				return err
			}); err != nil {
			return nil, fmt.Errorf("runner before hook: %w", err)
		}
	}

	// 2. Generate .env files from manifest
	envFiles := manifest.EnvFilePaths()
	if len(envFiles) > 0 {
		if err := m.step(ctx, "generate_env", "Generating .env files...", msg(".env files generated"), func() error {
			for relPath, envVars := range envFiles {
				content := RenderEnvFileContent(envVars)
				if err := ca.WriteFile(ctx, relPath, content, 0o644); err != nil {
					return fmt.Errorf("writing %s: %w", relPath, err)
				}
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("generating env files: %w", err)
		}
	}

	// 3. Start per-env infrastructure
	if err := m.step(ctx, "start_infra", "Starting infrastructure containers...", msg("Infrastructure containers started"), func() error {
		return m.compute.Start(ctx, envName, manifest.Ports)
	}); err != nil {
		return nil, fmt.Errorf("starting infrastructure: %w", err)
	}

	// 4. runner.deploy hook (typically skipped for local)
	if m.config.Runner != nil && m.config.Runner.Deploy != "" {
		deployMsg := fmt.Sprintf("Ran runner.deploy (%s)", m.config.Runner.Deploy)
		if err := m.step(ctx, "runner_deploy",
			fmt.Sprintf("Running runner.deploy → %s", m.config.Runner.Deploy),
			&deployMsg,
			func() error {
				m.progress.OnStep(StepEvent{Step: "runner_deploy", Status: StepStreaming})
				env := m.buildHookEnv(envName, ca.Root(), manifest.Ports)
				_, err := ca.Exec(ctx, m.config.Runner.Deploy, env)
				return err
			}); err != nil {
			return nil, fmt.Errorf("runner deploy hook: %w", err)
		}
	}

	// 5. runner.after hook
	if m.config.Runner != nil && m.config.Runner.After != "" {
		afterMsg := fmt.Sprintf("Ran runner.after (%s)", m.config.Runner.After)
		if err := m.step(ctx, "runner_after",
			fmt.Sprintf("Running runner.after → %s", m.config.Runner.After),
			&afterMsg,
			func() error {
				m.progress.OnStep(StepEvent{Step: "runner_after", Status: StepStreaming})
				env := m.buildHookEnv(envName, ca.Root(), manifest.Ports)
				_, err := ca.Exec(ctx, m.config.Runner.After, env)
				return err
			}); err != nil {
			return nil, fmt.Errorf("runner after hook: %w", err)
		}
	}

	// 6. Persist state
	now := time.Now()
	entry := &EnvironmentEntry{
		Name:               envName,
		Mode:               ModeLocal,
		Branch:             branch,
		Status:             StatusRunning,
		CreatedAt:          now,
		UpdatedAt:          now,
		Ports:              manifest.Ports,
		ProvisionerOutputs: manifest.ProvisionerOutputs,
		Compute: &ComputeAccessInfo{
			Type:            "local",
			Path:            ca.Root(),
			ManagedWorktree: managedWorktree,
		},
	}

	if err := m.step(ctx, "save_state", "Saving state...", msg("State saved"), func() error {
		return m.state.SetEnvironment(ctx, envName, entry)
	}); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}

	return entry, nil
}

// Destroy tears down an environment and cleans up all resources.
func (m *Manager) Destroy(ctx context.Context, envName string) error {
	// Load state first
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

	// Reconstruct ComputeAccess from state
	var ca ComputeAccess
	if entry.Compute != nil && entry.Compute.Path != "" {
		ca = NewDomainLocalComputeAccess(entry.Compute.Path)
	}

	// Run runner.destroy hook if defined
	if m.config.Runner != nil && m.config.Runner.Destroy != "" && ca != nil {
		destroyMsg := fmt.Sprintf("Ran runner.destroy (%s)", m.config.Runner.Destroy)
		if err := m.step(ctx, "runner_destroy",
			fmt.Sprintf("Running runner.destroy → %s", m.config.Runner.Destroy),
			&destroyMsg,
			func() error {
				m.progress.OnStep(StepEvent{Step: "runner_destroy", Status: StepStreaming})
				env := m.buildHookEnv(envName, ca.Root(), entry.Ports)
				_, err := ca.Exec(ctx, m.config.Runner.Destroy, env)
				return err
			}); err != nil {
			return fmt.Errorf("runner destroy hook: %w", err)
		}
	}

	// Run provisioner service "destroy" hooks (on orchestrator, not via ComputeAccess)
	for svcName, svc := range m.config.Provisioner.Services {
		if svc.Destroy == "" {
			continue
		}
		destroyMsg := fmt.Sprintf("Destroyed provisioner service %s", svcName)
		if err := m.step(ctx, fmt.Sprintf("destroy_core_%s", svcName),
			fmt.Sprintf("Running destroy hook for %s...", svcName),
			&destroyMsg,
			func() error {
				m.progress.OnStep(StepEvent{Step: fmt.Sprintf("destroy_core_%s", svcName), Status: StepStreaming})
				_, err := m.runCoreHook(ctx, svcName, "destroy", envName, entry.Ports)
				return err
			}); err != nil {
			return fmt.Errorf("destroying provisioner service %s: %w", svcName, err)
		}
	}

	// Destroy or stop compute
	if entry.IsManagedWorktree() {
		if err := m.step(ctx, "destroy_compute", "Removing worktree and stopping containers...", msg("Worktree and containers removed"), func() error {
			return m.compute.Destroy(ctx, envName)
		}); err != nil {
			return fmt.Errorf("destroying compute resources: %w", err)
		}
	} else {
		// Attached worktree — only stop containers, don't remove the worktree
		if err := m.step(ctx, "stop_infra", "Stopping infrastructure containers...", msg("Infrastructure containers stopped"), func() error {
			return m.compute.Stop(ctx, envName)
		}); err != nil {
			return fmt.Errorf("stopping infrastructure: %w", err)
		}
	}

	// Clean up .env files via os.Remove (simple cleanup, no ComputeAccess needed for local)
	if entry.Compute != nil && entry.Compute.Path != "" {
		if err := m.step(ctx, "cleanup_env", "Cleaning up env files...", msg("Env files cleaned up"), func() error {
			for _, svc := range m.config.Services {
				envFilePath := filepath.Join(entry.Compute.Path, svc.Path, svc.ResolvedEnvFile())
				_ = os.Remove(envFilePath) // ignore errors for missing files
			}
			return nil
		}); err != nil {
			return fmt.Errorf("cleaning up env files: %w", err)
		}
	}

	if err := m.step(ctx, "remove_state", "Removing state...", msg("State removed"), func() error {
		return m.state.RemoveEnvironment(ctx, envName)
	}); err != nil {
		return fmt.Errorf("removing state: %w", err)
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

// RunCoreHook runs a provisioner service hook for a given environment, loading ports from state.
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

// runCoreHook executes a provisioner service hook with proper env vars and returns captured outputs.
func (m *Manager) runCoreHook(ctx context.Context, svcName, action, envName string, ports PortMap) (map[string]string, error) {
	svc, ok := m.config.Provisioner.Services[svcName]
	if !ok {
		return nil, fmt.Errorf("unknown provisioner service '%s'", svcName)
	}

	var hookScript string
	switch action {
	case "init":
		hookScript = svc.Init
	case "seed":
		hookScript = svc.Seed
	case "reset":
		hookScript = svc.Reset
	case "destroy":
		hookScript = svc.Destroy
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

// buildHookEnv constructs environment variables for hooks.
func (m *Manager) buildHookEnv(envName, worktreePath string, ports PortMap) []string {
	env := append(os.Environ(),
		fmt.Sprintf("PREVIEWCTL_ENV_NAME=%s", envName),
		fmt.Sprintf("PREVIEWCTL_PROJECT_NAME=%s", m.config.Name),
		fmt.Sprintf("PREVIEWCTL_PROJECT_ROOT=%s", m.projectRoot),
	)
	if worktreePath != "" {
		env = append(env, fmt.Sprintf("PREVIEWCTL_WORKTREE_PATH=%s", worktreePath))
	}
	for name, port := range ports {
		env = append(env, fmt.Sprintf("PREVIEWCTL_PORT_%s=%d",
			strings.ToUpper(strings.ReplaceAll(name, "-", "_")), port))
	}
	return env
}

// CoreInit runs the "init" hook for a provisioner service (one-time setup).
func (m *Manager) CoreInit(ctx context.Context, svcName string) error {
	svc, ok := m.config.Provisioner.Services[svcName]
	if !ok {
		return fmt.Errorf("unknown provisioner service '%s'", svcName)
	}
	if svc.Init == "" {
		return fmt.Errorf("provisioner service '%s' has no init hook defined", svcName)
	}

	initMsg := fmt.Sprintf("Initialized provisioner service %s", svcName)
	if err := m.step(ctx, "core_init",
		fmt.Sprintf("Running init hook for %s...", svcName),
		&initMsg,
		func() error {
			m.progress.OnStep(StepEvent{Step: "core_init", Status: StepStreaming})
			_, err := m.runCoreHook(ctx, svcName, "init", "", nil)
			return err
		}); err != nil {
		return fmt.Errorf("initializing provisioner service %s: %w", svcName, err)
	}

	return nil
}

// CoreReset runs the "reset" hook for a provisioner service on a specific environment.
func (m *Manager) CoreReset(ctx context.Context, svcName, envName string) error {
	svc, ok := m.config.Provisioner.Services[svcName]
	if !ok {
		return fmt.Errorf("unknown provisioner service '%s'", svcName)
	}
	if svc.Reset == "" {
		return fmt.Errorf("provisioner service '%s' has no reset hook defined", svcName)
	}

	entry, err := m.state.GetEnvironment(ctx, envName)
	if err != nil {
		return fmt.Errorf("loading environment: %w", err)
	}
	if entry == nil {
		return fmt.Errorf("environment '%s' not found", envName)
	}

	resetMsg := fmt.Sprintf("Reset provisioner service %s for %s", svcName, envName)
	if err := m.step(ctx, "core_reset",
		fmt.Sprintf("Running reset hook for %s...", svcName),
		&resetMsg,
		func() error {
			m.progress.OnStep(StepEvent{Step: "core_reset", Status: StepStreaming})
			outputs, err := m.runCoreHook(ctx, svcName, "reset", envName, entry.Ports)
			if err != nil {
				return err
			}
			// Update stored outputs if hook produced new ones
			if outputs != nil {
				if entry.ProvisionerOutputs == nil {
					entry.ProvisionerOutputs = make(map[string]map[string]string)
				}
				entry.ProvisionerOutputs[svcName] = outputs
				return m.state.SetEnvironment(ctx, envName, entry)
			}
			return nil
		}); err != nil {
		return fmt.Errorf("resetting provisioner service %s: %w", svcName, err)
	}

	return nil
}

// DomainLocalComputeAccess is a minimal ComputeAccess implementation for use within
// the domain layer. It avoids importing the outbound/local package.
type DomainLocalComputeAccess struct {
	root string
}

// NewDomainLocalComputeAccess creates a local ComputeAccess from a filesystem path.
func NewDomainLocalComputeAccess(root string) ComputeAccess {
	return &DomainLocalComputeAccess{root: root}
}

func (l *DomainLocalComputeAccess) WriteFile(_ context.Context, relPath string, data []byte, mode os.FileMode) error {
	absPath := filepath.Join(l.root, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	if data == nil {
		_ = os.Remove(absPath)
		return nil
	}
	return os.WriteFile(absPath, data, mode)
}

func (l *DomainLocalComputeAccess) ReadFile(_ context.Context, relPath string) ([]byte, error) {
	return os.ReadFile(filepath.Join(l.root, relPath))
}

func (l *DomainLocalComputeAccess) Exec(ctx context.Context, command string, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = l.root
	cmd.Env = env
	cmd.Stderr = os.Stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("exec in %s: %w", l.root, err)
	}
	return stdout.String(), nil
}

func (l *DomainLocalComputeAccess) Root() string {
	return l.root
}
