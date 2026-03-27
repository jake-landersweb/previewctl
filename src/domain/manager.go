package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Manager orchestrates environment lifecycle by coordinating all ports.
type Manager struct {
	compute     ComputePort
	networking  NetworkingPort
	state       StatePort
	progress    ProgressReporter
	config      *ProjectConfig
	projectRoot string
	noCache     bool // when true, skip all checkpoint checks
}

// ManagerDeps holds the dependencies for creating a Manager.
type ManagerDeps struct {
	Compute     ComputePort
	Networking  NetworkingPort
	State       StatePort
	Progress    ProgressReporter
	Config      *ProjectConfig
	ProjectRoot string
	NoCache     bool
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
		noCache:     deps.NoCache,
	}
}

// SetNoCache sets whether checkpoint caching is disabled.
func (m *Manager) SetNoCache(v bool) { m.noCache = v }

// ---------- Step execution with checkpointing ----------

// VerifyFunc checks that a previously-completed step's side effects still hold.
type VerifyFunc func(ctx context.Context) error

// StepOpts configures step execution behavior.
type StepOpts struct {
	Name        string
	StartMsg    string
	CompleteMsg *string
	Fn          func() error
	Verify      VerifyFunc            // nil = pure, skip on checkpoint alone
	Outputs     func() map[string]any // capture outputs after success
}

// msg is a convenience for creating a string pointer for step().
func msg(s string) *string { return &s }

// step executes a lifecycle step with checkpoint-aware caching and audit logging.
// When entry is non-nil, completed steps are skipped (unless noCache is set) and
// results are persisted after each step.
func (m *Manager) step(ctx context.Context, entry *EnvironmentEntry, opts StepOpts) error {
	// Check for existing checkpoint
	if !m.noCache && entry != nil && entry.StepCompleted(opts.Name) {
		if opts.Verify != nil {
			// Stateful step: verify side effects still exist
			if err := opts.Verify(ctx); err != nil {
				// Verification failed — must re-execute
				entry.AppendAudit(AuditEntry{
					Timestamp: time.Now(),
					Step:      opts.Name,
					Action:    "verify_failed",
					Machine:   Hostname(),
					Message:   fmt.Sprintf("Re-executing: %v", err),
				})
				// Fall through to execute
			} else {
				// Verified — skip
				m.progress.OnStep(StepEvent{Step: opts.Name, Status: StepSkipped,
					Message: *opts.CompleteMsg + " (cached)"})
				entry.AppendAudit(AuditEntry{
					Timestamp: time.Now(),
					Step:      opts.Name,
					Action:    "verified",
					Machine:   Hostname(),
				})
				return nil
			}
		} else {
			// Pure step — skip without verification
			m.progress.OnStep(StepEvent{Step: opts.Name, Status: StepSkipped,
				Message: *opts.CompleteMsg + " (cached)"})
			entry.AppendAudit(AuditEntry{
				Timestamp: time.Now(),
				Step:      opts.Name,
				Action:    "skipped",
				Machine:   Hostname(),
			})
			return nil
		}
	}

	// Execute the step
	start := time.Now()
	m.progress.OnStep(StepEvent{Step: opts.Name, Status: StepStarted, Message: opts.StartMsg})

	if err := opts.Fn(); err != nil {
		m.progress.OnStep(StepEvent{Step: opts.Name, Status: StepFailed, Error: err})

		// Record failure
		if entry != nil {
			rec := &StepRecord{
				Name:       opts.Name,
				Status:     StepRecordFailed,
				StartedAt:  start,
				FinishedAt: time.Now(),
				DurationMs: time.Since(start).Milliseconds(),
				Machine:    Hostname(),
				Error:      err.Error(),
			}
			entry.SetStepRecord(rec)
			entry.AppendAudit(AuditEntry{
				Timestamp:  rec.FinishedAt,
				Step:       opts.Name,
				Action:     "failed",
				Machine:    rec.Machine,
				DurationMs: rec.DurationMs,
				Error:      err.Error(),
			})
			_ = m.state.SetEnvironment(ctx, entry.Name, entry) // best-effort
		}
		return err
	}

	m.progress.OnStep(StepEvent{Step: opts.Name, Status: StepCompleted, Message: *opts.CompleteMsg})

	// Record success
	if entry != nil {
		var outputs map[string]any
		if opts.Outputs != nil {
			outputs = opts.Outputs()
		}
		rec := &StepRecord{
			Name:       opts.Name,
			Status:     StepRecordCompleted,
			StartedAt:  start,
			FinishedAt: time.Now(),
			DurationMs: time.Since(start).Milliseconds(),
			Machine:    Hostname(),
			Outputs:    outputs,
		}
		entry.SetStepRecord(rec)
		entry.AppendAudit(AuditEntry{
			Timestamp:  rec.FinishedAt,
			Step:       opts.Name,
			Action:     "executed",
			Machine:    rec.Machine,
			DurationMs: rec.DurationMs,
		})
		// Reload store values from state in case a hook subprocess wrote to them
		if latest, err := m.state.GetEnvironment(ctx, entry.Name); err == nil && latest != nil && latest.Env != nil {
			entry.Env = latest.Env
		}
		if err := m.state.SetEnvironment(ctx, entry.Name, entry); err != nil {
			return fmt.Errorf("persisting step checkpoint: %w", err)
		}
	}

	return nil
}

// stepSimple executes a step without checkpointing (for meta-steps like save_state, remove_state).
func (m *Manager) stepSimple(_ context.Context, name string, startMsg string, completeMsg *string, fn func() error) error {
	m.progress.OnStep(StepEvent{Step: name, Status: StepStarted, Message: startMsg})
	if err := fn(); err != nil {
		m.progress.OnStep(StepEvent{Step: name, Status: StepFailed, Error: err})
		return err
	}
	m.progress.OnStep(StepEvent{Step: name, Status: StepCompleted, Message: *completeMsg})
	return nil
}

// computeAccessInfo builds ComputeAccessInfo from a ComputeAccess.
func computeAccessInfo(ca ComputeAccess, managed bool) *ComputeAccessInfo {
	if ssh, ok := ca.(*DomainSSHComputeAccess); ok {
		return &ComputeAccessInfo{
			Type:            "ssh",
			Host:            ssh.Host(),
			User:            ssh.User(),
			Path:            ssh.Root(),
			ManagedWorktree: managed,
		}
	}
	return &ComputeAccessInfo{
		Type:            "local",
		Path:            ca.Root(),
		ManagedWorktree: managed,
	}
}

// ---------- Step ordering ----------

// BuildProvisionerStepOrder returns the canonical step order for the provisioner phase.
func (m *Manager) BuildProvisionerStepOrder() []string {
	order := []string{"provisioner_before", "create_compute", "allocate_ports"}
	for svcName, svc := range m.config.Provisioner.Services {
		if svc.Seed != "" {
			order = append(order, fmt.Sprintf("seed_%s", svcName))
		}
	}
	order = append(order, "build_manifest", "write_manifest", "provisioner_after")
	return order
}

// BuildRunnerStepOrder returns the canonical step order for the runner phase.
func (m *Manager) BuildRunnerStepOrder() []string {
	steps := []string{"runner_before", "generate_env", "start_infra"}
	if m.config.Runner != nil && m.config.Runner.Compose != nil {
		steps = append(steps, "generate_compose", "generate_nginx", "build_services", "start_services")
	}
	steps = append(steps, "runner_deploy", "runner_after")
	return steps
}

// ---------- Output deserialization helpers ----------

// deserializePortMap converts a JSON-deserialized value back to PortMap.
func deserializePortMap(v any) PortMap {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	result := make(PortMap, len(m))
	for k, val := range m {
		if f, ok := val.(float64); ok {
			result[k] = int(f)
		}
	}
	return result
}

// deserializeStringMap converts a JSON-deserialized value back to map[string]string.
func deserializeStringMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			result[k] = s
		}
	}
	return result
}

// ---------- Public lifecycle methods ----------

// Init creates a new environment end-to-end: provisions then runs.
func (m *Manager) Init(ctx context.Context, envName string, branch string) (*EnvironmentEntry, error) {
	entry, err := m.Provision(ctx, envName, branch, "")
	if err != nil {
		return nil, err
	}
	ca, manifest, err := m.loadManifestFromEntry(ctx, entry)
	if err != nil {
		return nil, err
	}
	return m.runRunner(ctx, envName, branch, ca, manifest, true, true, entry)
}

// Attach creates a preview environment using an existing worktree.
func (m *Manager) Attach(ctx context.Context, envName string, worktreePath string) (*EnvironmentEntry, error) {
	entry, err := m.ProvisionAttach(ctx, envName, worktreePath, "")
	if err != nil {
		return nil, err
	}
	ca, manifest, err := m.loadManifestFromEntry(ctx, entry)
	if err != nil {
		return nil, err
	}
	return m.runRunner(ctx, envName, entry.Branch, ca, manifest, false, true, entry)
}

// Provision runs the provisioner phase only. Does NOT run the runner.
// fromStep invalidates that step and all subsequent steps, forcing re-execution.
func (m *Manager) Provision(ctx context.Context, envName, branch, fromStep string) (*EnvironmentEntry, error) {
	ca, manifest, entry, err := m.runProvisioner(ctx, envName, branch, "", true, fromStep)
	if err != nil {
		return nil, err
	}
	return m.saveProvisionedState(ctx, envName, branch, ca, manifest, true, entry)
}

// ProvisionAttach runs the provisioner phase on an existing worktree.
func (m *Manager) ProvisionAttach(ctx context.Context, envName, worktreePath, fromStep string) (*EnvironmentEntry, error) {
	branch, err := m.compute.DetectBranch(ctx, worktreePath)
	if err != nil {
		return nil, fmt.Errorf("detecting branch: %w", err)
	}
	existing, err := m.state.GetEnvironment(ctx, envName)
	if err != nil {
		return nil, fmt.Errorf("checking existing state: %w", err)
	}
	// Allow re-provisioning an existing env (for resume), but not a brand new duplicate
	if existing != nil && existing.Status == StatusRunning {
		return nil, fmt.Errorf("environment '%s' is already running — use 'delete' first or choose a different name", envName)
	}

	ca, manifest, entry, err := m.runProvisioner(ctx, envName, branch, worktreePath, false, fromStep)
	if err != nil {
		return nil, err
	}
	return m.saveProvisionedState(ctx, envName, branch, ca, manifest, false, entry)
}

// Run reads a manifest and executes the runner phase only. Stateless — does not persist state.
func (m *Manager) Run(ctx context.Context, manifestPath, fromStep string) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading manifest: %w", err)
	}
	manifest, err := ReadManifest(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parsing manifest: %w", err)
	}

	rootDir := filepath.Dir(manifestPath)
	if rootDir == "" || rootDir == "." {
		rootDir, _ = os.Getwd()
	}
	ca := NewDomainLocalComputeAccess(rootDir)

	// For stateless run, entry is nil — no checkpointing
	_, err = m.runRunner(ctx, manifest.EnvName, manifest.Branch, ca, manifest, false, false, nil)
	return err
}

// RunStep executes a single runner-phase step in isolation.
// Loads the environment from state, reconstructs compute access, reads the manifest,
// and runs the specified step without checkpointing (always executes).
func (m *Manager) RunStep(ctx context.Context, envName, stepName string) error {
	entry, err := m.state.GetEnvironment(ctx, envName)
	if err != nil {
		return fmt.Errorf("loading environment: %w", err)
	}
	if entry == nil {
		return fmt.Errorf("environment '%s' not found", envName)
	}

	ca, manifest, err := m.loadManifestFromEntry(ctx, entry)
	if err != nil {
		return err
	}

	// Temporarily enable noCache so the step always runs
	origNoCache := m.noCache
	m.noCache = true
	defer func() { m.noCache = origNoCache }()

	// Run the single step through the runner, which will skip all other cached steps
	// and only execute the target step (since noCache is true, it runs everything,
	// but we want just one step). Instead, call runRunner and let it execute —
	// the noCache flag means nothing is skipped.
	// TODO: For true single-step execution, we'd need to extract each step into
	// a callable function. For now, run the full runner with noCache.
	_, err = m.runRunner(ctx, envName, entry.Branch, ca, manifest, false, true, entry)
	return err
}

// SetStatus updates an environment's status.
func (m *Manager) SetStatus(ctx context.Context, envName string, status EnvironmentStatus) error {
	entry, err := m.state.GetEnvironment(ctx, envName)
	if err != nil {
		return fmt.Errorf("loading environment: %w", err)
	}
	if entry == nil {
		return fmt.Errorf("environment '%s' not found", envName)
	}
	entry.Status = status
	entry.UpdatedAt = time.Now()
	return m.state.SetEnvironment(ctx, envName, entry)
}

// ---------- Internal provisioner / runner ----------

func (m *Manager) saveProvisionedState(ctx context.Context, envName, branch string, ca ComputeAccess, manifest *Manifest, managedWorktree bool, entry *EnvironmentEntry) (*EnvironmentEntry, error) {
	entry.Mode = EnvironmentMode(manifest.Mode)
	entry.Branch = branch
	entry.Status = StatusProvisioned
	entry.Ports = manifest.Ports
	entry.ProvisionerOutputs = manifest.ProvisionerOutputs
	entry.Compute = computeAccessInfo(ca, managedWorktree)
	entry.UpdatedAt = time.Now()

	if err := m.stepSimple(ctx, "save_state", "Saving state...", msg("State saved"), func() error {
		return m.state.SetEnvironment(ctx, envName, entry)
	}); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}
	return entry, nil
}

func (m *Manager) loadManifestFromEntry(ctx context.Context, entry *EnvironmentEntry) (ComputeAccess, *Manifest, error) {
	if entry.Compute == nil {
		return nil, nil, fmt.Errorf("environment '%s' has no compute info", entry.Name)
	}

	var ca ComputeAccess
	if entry.Compute.Type == "ssh" {
		ca = NewDomainSSHComputeAccess(entry.Compute.Host, entry.Compute.User, entry.Compute.Path)
	} else {
		ca = NewDomainLocalComputeAccess(entry.Compute.Path)
	}

	data, err := ca.ReadFile(ctx, ".previewctl.json")
	if err != nil {
		return nil, nil, fmt.Errorf("reading manifest from compute: %w", err)
	}
	manifest, err := ReadManifest(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("parsing manifest: %w", err)
	}
	return ca, manifest, nil
}

// runProvisioner executes the provisioner phase with step-level checkpointing.
func (m *Manager) runProvisioner(ctx context.Context, envName, branch, existingWorktree string, createWorktree bool, fromStep string) (ComputeAccess, *Manifest, *EnvironmentEntry, error) {
	// Load or create entry for checkpointing
	entry, err := m.state.GetEnvironment(ctx, envName)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("loading state: %w", err)
	}
	if entry == nil {
		entry = &EnvironmentEntry{
			Name:      envName,
			Mode:      EnvironmentMode(m.config.Mode),
			Branch:    branch,
			Status:    StatusCreating,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Steps:     make(map[string]*StepRecord),
		}
		if err := m.state.SetEnvironment(ctx, envName, entry); err != nil {
			return nil, nil, nil, fmt.Errorf("saving initial state: %w", err)
		}
	}

	// Invalidate from a specific step if requested
	if fromStep != "" {
		order := m.BuildProvisionerStepOrder()
		entry.InvalidateStepsFrom(fromStep, order)
		if err := m.state.SetEnvironment(ctx, envName, entry); err != nil {
			return nil, nil, nil, fmt.Errorf("saving invalidated state: %w", err)
		}
	}

	// 1. provisioner.before hook
	if m.config.Provisioner.Before != "" {
		beforeMsg := fmt.Sprintf("Ran provisioner.before (%s)", m.config.Provisioner.Before)
		if err := m.step(ctx, entry, StepOpts{
			Name:        "provisioner_before",
			StartMsg:    fmt.Sprintf("Running provisioner.before → %s", m.config.Provisioner.Before),
			CompleteMsg: &beforeMsg,
			Fn: func() error {
				m.progress.OnStep(StepEvent{Step: "provisioner_before", Status: StepStreaming, Message: fmt.Sprintf("Running provisioner.before → %s", m.config.Provisioner.Before)})
				env := m.buildHookEnv(envName, existingWorktree, nil, entry.Env)
				_, err := ExecuteCoreHook(ctx, m.config.Provisioner.Before, nil, env, m.projectRoot, m.progress.StderrWriter())
				return err
			},
		}); err != nil {
			return nil, nil, nil, fmt.Errorf("provisioner before hook: %w", err)
		}
	}

	// 2-3. Create compute and construct ComputeAccess
	var ca ComputeAccess
	if m.config.Provisioner.Compute != nil && m.config.Provisioner.Compute.Create != "" {
		// Remote mode
		var computeOutputs map[string]string
		// Try to load cached compute info
		if entry.Compute != nil && entry.Compute.Type == "ssh" && entry.StepCompleted("create_compute") {
			ca = NewDomainSSHComputeAccess(entry.Compute.Host, entry.Compute.User, entry.Compute.Path)
		}
		createMsg := "Compute created via hook"
		if err := m.step(ctx, entry, StepOpts{
			Name:        "create_compute",
			StartMsg:    "Creating remote compute...",
			CompleteMsg: &createMsg,
			Fn: func() error {
				m.progress.OnStep(StepEvent{Step: "create_compute", Status: StepStreaming, Message: "Creating remote compute..."})
				env := m.buildHookEnv(envName, "", nil, entry.Env)
				env = append(env, fmt.Sprintf("PREVIEWCTL_BRANCH=%s", branch))
				var err error
				computeOutputs, err = ExecuteCoreHook(ctx, m.config.Provisioner.Compute.Create,
					m.config.Provisioner.Compute.Outputs, env, m.projectRoot, m.progress.StderrWriter())
				return err
			},
			Verify: func(ctx context.Context) error {
				if ca == nil {
					return fmt.Errorf("no cached compute access")
				}
				_, err := ca.Exec(ctx, "echo ok", nil)
				return err
			},
			Outputs: func() map[string]any {
				return map[string]any{
					"VM_IP":       computeOutputs["VM_IP"],
					"SSH_USER":    computeOutputs["SSH_USER"],
					"REMOTE_ROOT": computeOutputs["REMOTE_ROOT"],
				}
			},
		}); err != nil {
			return nil, nil, nil, fmt.Errorf("compute create hook: %w", err)
		}
		// Construct CA from outputs (whether freshly executed or loaded from cache)
		if computeOutputs != nil {
			host := computeOutputs["VM_IP"]
			user := computeOutputs["SSH_USER"]
			root := computeOutputs["REMOTE_ROOT"]
			if root == "" {
				root = "/app"
			}
			ca = NewDomainSSHComputeAccess(host, user, root)
			// Update entry compute info for subsequent steps
			entry.Compute = computeAccessInfo(ca, true)
		}
	} else if existingWorktree != "" {
		ca = NewDomainLocalComputeAccess(existingWorktree)
	} else if createWorktree {
		// Load cached worktree path if available
		if entry.Compute != nil && entry.Compute.Type == "local" && entry.Compute.Path != "" && entry.StepCompleted("create_compute") {
			ca = NewDomainLocalComputeAccess(entry.Compute.Path)
		}
		var resources *ComputeResources
		if err := m.step(ctx, entry, StepOpts{
			Name:        "create_compute",
			StartMsg:    "Creating compute resources...",
			CompleteMsg: msg("Compute resources created"),
			Fn: func() error {
				var err error
				resources, err = m.compute.Create(ctx, envName, branch)
				return err
			},
			Verify: func(ctx context.Context) error {
				if ca == nil {
					return fmt.Errorf("no cached compute path")
				}
				if _, err := os.Stat(ca.Root()); err != nil {
					return fmt.Errorf("worktree missing: %w", err)
				}
				return nil
			},
			Outputs: func() map[string]any {
				return map[string]any{"worktreePath": resources.WorktreePath}
			},
		}); err != nil {
			return nil, nil, nil, fmt.Errorf("creating compute resources: %w", err)
		}
		if resources != nil {
			ca = NewDomainLocalComputeAccess(resources.WorktreePath)
			entry.Compute = computeAccessInfo(ca, true)
		}
	}

	// Wire stderr through the progress reporter for indented output
	if setter, ok := ca.(interface{ SetStderr(io.Writer) }); ok {
		setter.SetStderr(m.progress.StderrWriter())
	}

	// 4. Allocate ports
	var ports PortMap
	// Load cached ports if step was completed
	if entry.StepCompleted("allocate_ports") {
		if cached := entry.StepOutputs("allocate_ports"); cached != nil {
			if p, ok := cached["ports"]; ok {
				ports = deserializePortMap(p)
			}
		}
	}
	if err := m.step(ctx, entry, StepOpts{
		Name:        "allocate_ports",
		StartMsg:    "Allocating ports...",
		CompleteMsg: msg("Ports allocated"),
		Fn: func() error {
			var err error
			ports, err = m.networking.AllocatePorts(envName)
			if err != nil {
				return err
			}
			// Override with fixed ports from config
			for name, svc := range m.config.Services {
				if svc.Port != 0 {
					ports[name] = svc.Port
				}
			}
			return nil
		},
		Outputs: func() map[string]any {
			return map[string]any{"ports": ports}
		},
	}); err != nil {
		return nil, nil, nil, fmt.Errorf("allocating ports: %w", err)
	}

	// 5. Run provisioner service "seed" hooks
	provisionerOutputs := make(map[string]map[string]string)
	for svcName, svc := range m.config.Provisioner.Services {
		if svc.Seed == "" {
			continue
		}
		stepName := fmt.Sprintf("seed_%s", svcName)
		// Load cached outputs
		if entry.StepCompleted(stepName) {
			if cached := entry.StepOutputs(stepName); cached != nil {
				if o, ok := cached["outputs"]; ok {
					provisionerOutputs[svcName] = deserializeStringMap(o)
				}
			}
		}
		seedMsg := fmt.Sprintf("Seeded %s (%s)", svcName, svc.Seed)
		svcNameCopy := svcName // capture for closure
		if err := m.step(ctx, entry, StepOpts{
			Name:        stepName,
			StartMsg:    fmt.Sprintf("Seeding %s → %s", svcName, svc.Seed),
			CompleteMsg: &seedMsg,
			Fn: func() error {
				m.progress.OnStep(StepEvent{Step: stepName, Status: StepStreaming, Message: fmt.Sprintf("Seeding %s → %s", svcNameCopy, svc.Seed)})
				outputs, err := m.runCoreHook(ctx, svcNameCopy, "seed", envName, ports)
				if err != nil {
					return err
				}
				provisionerOutputs[svcNameCopy] = outputs
				return nil
			},
			Outputs: func() map[string]any {
				return map[string]any{"outputs": provisionerOutputs[svcNameCopy]}
			},
		}); err != nil {
			return nil, nil, nil, fmt.Errorf("seeding %s: %w", svcName, err)
		}
	}

	// 6. Build manifest
	mode := m.config.Mode
	if mode == "" {
		mode = "local"
	}
	var manifest *Manifest
	if err := m.step(ctx, entry, StepOpts{
		Name:        "build_manifest",
		StartMsg:    "Building manifest...",
		CompleteMsg: msg("Manifest built"),
		Fn: func() error {
			var err error
			manifest, err = BuildManifest(m.config, envName, branch, mode, ports, provisionerOutputs, entry.Env)
			return err
		},
	}); err != nil {
		return nil, nil, nil, fmt.Errorf("building manifest: %w", err)
	}
	// If build_manifest was skipped, we still need the manifest object
	if manifest == nil {
		var err error
		manifest, err = BuildManifest(m.config, envName, branch, mode, ports, provisionerOutputs, entry.Env)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("rebuilding manifest after cache: %w", err)
		}
	}

	// 7. Write .previewctl.json
	if err := m.step(ctx, entry, StepOpts{
		Name:        "write_manifest",
		StartMsg:    "Writing manifest...",
		CompleteMsg: msg("Manifest written to .previewctl.json"),
		Fn: func() error {
			data, err := json.MarshalIndent(manifest, "", "  ")
			if err != nil {
				return err
			}
			return ca.WriteFile(ctx, ".previewctl.json", data, 0o644)
		},
	}); err != nil {
		return nil, nil, nil, fmt.Errorf("writing manifest: %w", err)
	}

	// 8. provisioner.after hook
	if m.config.Provisioner.After != "" {
		afterMsg := fmt.Sprintf("Ran provisioner.after (%s)", m.config.Provisioner.After)
		if err := m.step(ctx, entry, StepOpts{
			Name:        "provisioner_after",
			StartMsg:    fmt.Sprintf("Running provisioner.after → %s", m.config.Provisioner.After),
			CompleteMsg: &afterMsg,
			Fn: func() error {
				m.progress.OnStep(StepEvent{Step: "provisioner_after", Status: StepStreaming, Message: fmt.Sprintf("Running provisioner.after → %s", m.config.Provisioner.After)})
				env := m.buildHookEnv(envName, ca.Root(), manifest.Ports, entry.Env)
				_, err := ExecuteCoreHook(ctx, m.config.Provisioner.After, nil, env, m.projectRoot, m.progress.StderrWriter())
				return err
			},
		}); err != nil {
			return nil, nil, nil, fmt.Errorf("provisioner after hook: %w", err)
		}
	}

	return ca, manifest, entry, nil
}

// runRunner executes the runner phase with optional checkpointing.
// When entry is nil, no checkpointing occurs (stateless mode).
func (m *Manager) runRunner(ctx context.Context, envName, branch string, ca ComputeAccess, manifest *Manifest, managedWorktree, saveState bool, entry *EnvironmentEntry) (*EnvironmentEntry, error) {
	// Wire stderr through the progress reporter
	if setter, ok := ca.(interface{ SetStderr(io.Writer) }); ok {
		setter.SetStderr(m.progress.StderrWriter())
	}

	// 0. Sync code on re-runs (pull latest from remote)
	// Only runs when the runner has already completed before (re-run, not first create).
	// Runs directly (not via step()) so it always executes — the point is to pull fresh code.
	if entry != nil && entry.StepCompleted("runner_before") {
		m.progress.OnStep(StepEvent{Step: "sync_code", Status: StepStarted, Message: fmt.Sprintf("Syncing code to origin/%s...", branch)})
		syncCmd := fmt.Sprintf("git fetch --depth 1 origin %s && git reset --hard origin/%s", branch, branch)
		if _, err := ca.Exec(ctx, syncCmd, nil); err != nil {
			m.progress.OnStep(StepEvent{Step: "sync_code", Status: StepFailed, Message: err.Error()})
			return nil, fmt.Errorf("syncing code: %w", err)
		}
		m.progress.OnStep(StepEvent{Step: "sync_code", Status: StepCompleted, Message: "Code synced to latest"})
	}

	// 1. runner.before hook
	if m.config.Runner != nil && m.config.Runner.Before != "" {
		beforeMsg := fmt.Sprintf("Ran runner.before (%s)", m.config.Runner.Before)
		if err := m.step(ctx, entry, StepOpts{
			Name:        "runner_before",
			StartMsg:    fmt.Sprintf("Running runner.before → %s", m.config.Runner.Before),
			CompleteMsg: &beforeMsg,
			Fn: func() error {
				m.progress.OnStep(StepEvent{Step: "runner_before", Status: StepStreaming, Message: fmt.Sprintf("Running runner.before → %s", m.config.Runner.Before)})
				env := m.buildHookEnv(envName, ca.Root(), manifest.Ports, entry.Env)
				_, err := ca.Exec(ctx, m.config.Runner.Before, env)
				return err
			},
		}); err != nil {
			return nil, fmt.Errorf("runner before hook: %w", err)
		}
	}

	// 2. Generate .env files from manifest (batched into a single SSH call)
	envFiles := manifest.EnvFilePaths()
	if len(envFiles) > 0 {
		if err := m.step(ctx, entry, StepOpts{
			Name:        "generate_env",
			StartMsg:    "Generating .env files...",
			CompleteMsg: msg(".env files generated"),
			Fn: func() error {
				// Build a single script that writes all env files at once
				var script strings.Builder
				script.WriteString("set -e\n")
				for relPath, envVars := range envFiles {
					content := RenderEnvFileContent(envVars)
					dir := filepath.Dir(relPath)
					if dir != "." {
						fmt.Fprintf(&script, "mkdir -p %q\n", dir)
					}
					fmt.Fprintf(&script, "cat > %q <<'ENVEOF'\n%sENVEOF\n", relPath, string(content))
				}
				_, err := ca.Exec(ctx, script.String(), nil)
				return err
			},
		}); err != nil {
			return nil, fmt.Errorf("generating env files: %w", err)
		}
	}

	// 3. Start per-env infrastructure
	if err := m.step(ctx, entry, StepOpts{
		Name:        "start_infra",
		StartMsg:    "Starting infrastructure containers...",
		CompleteMsg: msg("Infrastructure containers started"),
		Fn: func() error {
			composeFile := ""
			if manifest.Infrastructure != nil {
				composeFile = manifest.Infrastructure.ComposeFile
			}
			if composeFile == "" {
				return nil
			}
			env := BuildComposeEnv(m.config.Name, envName, manifest.Ports)
			cmd := fmt.Sprintf("docker compose -f %s up -d", composeFile)
			_, err := ca.Exec(ctx, cmd, env)
			return err
		},
		Verify: func(ctx context.Context) error {
			composeFile := ""
			if manifest.Infrastructure != nil {
				composeFile = manifest.Infrastructure.ComposeFile
			}
			if composeFile == "" {
				return nil
			}
			projectName := ComposeProjectName(m.config.Name, envName)
			cmd := fmt.Sprintf("docker compose -f %s -p %s ps --format json", composeFile, projectName)
			out, err := ca.Exec(ctx, cmd, nil)
			if err != nil {
				return err
			}
			if len(strings.TrimSpace(out)) == 0 {
				return fmt.Errorf("infrastructure not running")
			}
			return nil
		},
	}); err != nil {
		return nil, fmt.Errorf("starting infrastructure: %w", err)
	}

	// 4-7. Compose-managed services (only when runner.compose is configured)
	if m.config.Runner != nil && m.config.Runner.Compose != nil {
		// 4. Generate Docker Compose file
		if err := m.step(ctx, entry, StepOpts{
			Name:        "generate_compose",
			StartMsg:    "Generating Docker Compose file...",
			CompleteMsg: msg("Docker Compose file generated"),
			Fn: func() error {
				data, err := GenerateComposeFile(m.config, manifest)
				if err != nil {
					return fmt.Errorf("generating compose file: %w", err)
				}
				return ca.WriteFile(ctx, ".previewctl.compose.yaml", data, 0o644)
			},
		}); err != nil {
			return nil, fmt.Errorf("generating compose file: %w", err)
		}

		// 5. Generate proxy config (nginx, etc.) if enabled
		if m.config.Runner.Compose.Proxy.IsEnabled() {
			if err := m.step(ctx, entry, StepOpts{
				Name:        "generate_nginx",
				StartMsg:    "Generating nginx config...",
				CompleteMsg: msg("nginx config generated"),
				Fn: func() error {
					domain := m.config.Runner.Compose.Proxy.Domain
					if domain == "" {
						return fmt.Errorf("runner.compose.proxy.domain is required")
					}
					data, err := GenerateNginxConfig(m.config, manifest, domain)
					if err != nil {
						return fmt.Errorf("generating nginx config: %w", err)
					}
					return ca.WriteFile(ctx, "preview/nginx.conf", data, 0o644)
				},
			}); err != nil {
				return nil, fmt.Errorf("generating nginx config: %w", err)
			}
		}

		// 6. Build autostart services (batched into single SSH call)
		if err := m.step(ctx, entry, StepOpts{
			Name:        "build_services",
			StartMsg:    "Building services...",
			CompleteMsg: msg("Services built"),
			Fn: func() error {
				m.progress.OnStep(StepEvent{Step: "build_services", Status: StepStreaming})
				var cmds []string
				for _, svcName := range m.config.Runner.Compose.Autostart {
					svc, ok := m.config.Services[svcName]
					if !ok || svc.Build == "" {
						continue
					}
					cmds = append(cmds, svc.Build)
				}
				if len(cmds) == 0 {
					return nil
				}
				_, err := ca.Exec(ctx, strings.Join(cmds, " && "), nil)
				return err
			},
		}); err != nil {
			return nil, fmt.Errorf("building services: %w", err)
		}

		// 7. Start autostart services (+ proxy if enabled)
		if err := m.step(ctx, entry, StepOpts{
			Name:        "start_services",
			StartMsg:    "Starting services...",
			CompleteMsg: msg("Services started"),
			Fn: func() error {
				var services []string
				if m.config.Runner.Compose.Proxy.IsEnabled() {
					proxyType := m.config.Runner.Compose.Proxy.ResolvedType()
					services = append(services, proxyType) // e.g., "nginx"
				}
				services = append(services, m.config.Runner.Compose.Autostart...)
				cmd := fmt.Sprintf("docker compose -f .previewctl.compose.yaml up -d %s", strings.Join(services, " "))
				_, err := ca.Exec(ctx, cmd, nil)
				return err
			},
		}); err != nil {
			return nil, fmt.Errorf("starting services: %w", err)
		}
	}

	// 8. runner.deploy hook
	if m.config.Runner != nil && m.config.Runner.Deploy != "" {
		deployMsg := fmt.Sprintf("Ran runner.deploy (%s)", m.config.Runner.Deploy)
		if err := m.step(ctx, entry, StepOpts{
			Name:        "runner_deploy",
			StartMsg:    fmt.Sprintf("Running runner.deploy → %s", m.config.Runner.Deploy),
			CompleteMsg: &deployMsg,
			Fn: func() error {
				m.progress.OnStep(StepEvent{Step: "runner_deploy", Status: StepStreaming, Message: fmt.Sprintf("Running runner.deploy → %s", m.config.Runner.Deploy)})
				env := m.buildHookEnv(envName, ca.Root(), manifest.Ports, entry.Env)
				_, err := ca.Exec(ctx, m.config.Runner.Deploy, env)
				return err
			},
		}); err != nil {
			return nil, fmt.Errorf("runner deploy hook: %w", err)
		}
	}

	// 5. runner.after hook
	if m.config.Runner != nil && m.config.Runner.After != "" {
		afterMsg := fmt.Sprintf("Ran runner.after (%s)", m.config.Runner.After)
		if err := m.step(ctx, entry, StepOpts{
			Name:        "runner_after",
			StartMsg:    fmt.Sprintf("Running runner.after → %s", m.config.Runner.After),
			CompleteMsg: &afterMsg,
			Fn: func() error {
				m.progress.OnStep(StepEvent{Step: "runner_after", Status: StepStreaming, Message: fmt.Sprintf("Running runner.after → %s", m.config.Runner.After)})
				env := m.buildHookEnv(envName, ca.Root(), manifest.Ports, entry.Env)
				_, err := ca.Exec(ctx, m.config.Runner.After, env)
				return err
			},
		}); err != nil {
			return nil, fmt.Errorf("runner after hook: %w", err)
		}
	}

	// 6. Save state
	if !saveState {
		return nil, nil
	}

	now := time.Now()
	if entry == nil {
		entry = &EnvironmentEntry{}
	}
	entry.Name = envName
	entry.Mode = EnvironmentMode(manifest.Mode)
	entry.Branch = branch
	entry.Status = StatusRunning
	entry.Ports = manifest.Ports
	entry.ProvisionerOutputs = manifest.ProvisionerOutputs
	entry.Compute = computeAccessInfo(ca, managedWorktree)
	entry.UpdatedAt = now
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}

	if err := m.stepSimple(ctx, "save_state", "Saving state...", msg("State saved"), func() error {
		return m.state.SetEnvironment(ctx, envName, entry)
	}); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}

	return entry, nil
}

// ---------- Destroy ----------

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
	m.progress.OnStep(StepEvent{Step: "load_state", Status: StepCompleted, Message: "Environment state loaded"})

	// Reconstruct ComputeAccess from state
	var ca ComputeAccess
	if entry.Compute != nil {
		switch entry.Compute.Type {
		case "ssh":
			ca = NewDomainSSHComputeAccess(entry.Compute.Host, entry.Compute.User, entry.Compute.Path)
		case "local":
			if entry.Compute.Path != "" {
				ca = NewDomainLocalComputeAccess(entry.Compute.Path)
			}
		}
	}

	// runner.destroy hook
	if m.config.Runner != nil && m.config.Runner.Destroy != "" && ca != nil {
		destroyMsg := fmt.Sprintf("Ran runner.destroy (%s)", m.config.Runner.Destroy)
		if err := m.stepSimple(ctx, "runner_destroy",
			fmt.Sprintf("Running runner.destroy → %s", m.config.Runner.Destroy),
			&destroyMsg,
			func() error {
				m.progress.OnStep(StepEvent{Step: "runner_destroy", Status: StepStreaming, Message: fmt.Sprintf("Running runner.destroy → %s", m.config.Runner.Destroy)})
				env := m.buildHookEnv(envName, ca.Root(), entry.Ports, entry.Env)
				_, err := ca.Exec(ctx, m.config.Runner.Destroy, env)
				return err
			}); err != nil {
			return fmt.Errorf("runner destroy hook: %w", err)
		}
	}

	// Provisioner service destroy hooks
	for svcName, svc := range m.config.Provisioner.Services {
		if svc.Destroy == "" {
			continue
		}
		destroyMsg := fmt.Sprintf("Destroyed provisioner service %s", svcName)
		if err := m.stepSimple(ctx, fmt.Sprintf("destroy_core_%s", svcName),
			fmt.Sprintf("Running destroy hook for %s...", svcName),
			&destroyMsg,
			func() error {
				m.progress.OnStep(StepEvent{Step: fmt.Sprintf("destroy_core_%s", svcName), Status: StepStreaming, Message: fmt.Sprintf("Running destroy hook for %s...", svcName)})
				_, err := m.runCoreHook(ctx, svcName, "destroy", envName, entry.Ports, entry.Env)
				return err
			}); err != nil {
			return fmt.Errorf("destroying provisioner service %s: %w", svcName, err)
		}
	}

	// Compute destroy hook (remote) or worktree removal (local)
	if m.config.Provisioner.Compute != nil && m.config.Provisioner.Compute.Destroy != "" {
		destroyMsg := "Compute destroyed via hook"
		if err := m.stepSimple(ctx, "destroy_compute_hook", "Destroying remote compute...", &destroyMsg, func() error {
			m.progress.OnStep(StepEvent{Step: "destroy_compute_hook", Status: StepStreaming, Message: "Destroying remote compute..."})
			env := m.buildHookEnv(envName, "", entry.Ports, entry.Env)
			if entry.Compute != nil {
				env = append(env,
					fmt.Sprintf("PREVIEWCTL_VM_IP=%s", entry.Compute.Host),
					fmt.Sprintf("PREVIEWCTL_SSH_USER=%s", entry.Compute.User),
				)
			}
			_, err := ExecuteCoreHook(ctx, m.config.Provisioner.Compute.Destroy, nil, env, m.projectRoot, m.progress.StderrWriter())
			return err
		}); err != nil {
			return fmt.Errorf("compute destroy hook: %w", err)
		}
	} else if entry.Compute != nil && entry.Compute.Type == "local" {
		if entry.IsManagedWorktree() {
			if err := m.stepSimple(ctx, "destroy_compute", "Removing worktree and stopping containers...", msg("Worktree and containers removed"), func() error {
				return m.compute.Destroy(ctx, envName)
			}); err != nil {
				return fmt.Errorf("destroying compute resources: %w", err)
			}
		} else {
			if err := m.stepSimple(ctx, "stop_infra", "Stopping infrastructure containers...", msg("Infrastructure containers stopped"), func() error {
				return m.compute.Stop(ctx, envName)
			}); err != nil {
				return fmt.Errorf("stopping infrastructure: %w", err)
			}
		}
	}

	// Clean up .env files for local compute
	if entry.Compute != nil && entry.Compute.Type == "local" && entry.Compute.Path != "" {
		if err := m.stepSimple(ctx, "cleanup_env", "Cleaning up env files...", msg("Env files cleaned up"), func() error {
			for _, svc := range m.config.Services {
				envFilePath := filepath.Join(entry.Compute.Path, svc.Path, svc.ResolvedEnvFile())
				_ = os.Remove(envFilePath)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("cleaning up env files: %w", err)
		}
	}

	if err := m.stepSimple(ctx, "remove_state", "Removing state...", msg("State removed"), func() error {
		return m.state.RemoveEnvironment(ctx, envName)
	}); err != nil {
		return fmt.Errorf("removing state: %w", err)
	}

	return nil
}

// ---------- Query methods ----------

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

// GetEnvironment returns a single environment entry from state, or nil if not found.
func (m *Manager) GetEnvironment(ctx context.Context, envName string) (*EnvironmentEntry, error) {
	return m.state.GetEnvironment(ctx, envName)
}

// SaveEnvironment persists an environment entry to state.
func (m *Manager) SaveEnvironment(ctx context.Context, envName string, entry *EnvironmentEntry) error {
	return m.state.SetEnvironment(ctx, envName, entry)
}

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
	return &EnvironmentDetail{Entry: entry, InfraRunning: infraRunning}, nil
}

// ---------- Provisioner service hooks ----------

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

func (m *Manager) runCoreHook(ctx context.Context, svcName, action, envName string, ports PortMap, store ...map[string]string) (map[string]string, error) {
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
	env := append(os.Environ(),
		fmt.Sprintf("PREVIEWCTL_ENV_NAME=%s", envName),
		fmt.Sprintf("PREVIEWCTL_ENVIRONMENT_NAME=%s", SanitizeName(envName)),
		fmt.Sprintf("PREVIEWCTL_ACTION=%s", action),
		fmt.Sprintf("PREVIEWCTL_PROJECT_NAME=%s", m.config.Name),
		fmt.Sprintf("PREVIEWCTL_SERVICE_NAME=%s", svcName),
		fmt.Sprintf("PREVIEWCTL_PROJECT_ROOT=%s", m.projectRoot),
	)
	for name, port := range ports {
		env = append(env, fmt.Sprintf("PREVIEWCTL_PORT_%s=%d",
			strings.ToUpper(strings.ReplaceAll(name, "-", "_")), port))
	}
	// Inject persistent store values
	if len(store) > 0 && store[0] != nil {
		for k, v := range store[0] {
			env = append(env, fmt.Sprintf("PREVIEWCTL_STORE_%s=%s",
				strings.ToUpper(strings.ReplaceAll(k, "-", "_")), v))
		}
	}
	var requiredOutputs []string
	if action == "seed" || action == "reset" {
		requiredOutputs = svc.Outputs
	}
	return ExecuteCoreHook(ctx, hookScript, requiredOutputs, env, m.projectRoot, m.progress.StderrWriter())
}

func (m *Manager) buildHookEnv(envName, worktreePath string, ports PortMap, envStore ...map[string]string) []string {
	env := append(os.Environ(),
		fmt.Sprintf("PREVIEWCTL_ENV_NAME=%s", envName),
		fmt.Sprintf("PREVIEWCTL_ENVIRONMENT_NAME=%s", SanitizeName(envName)),
		fmt.Sprintf("PREVIEWCTL_PROJECT_NAME=%s", m.config.Name),
		fmt.Sprintf("PREVIEWCTL_PROJECT_ROOT=%s", m.projectRoot),
		fmt.Sprintf("PREVIEWCTL_MODE=%s", m.config.Mode),
	)
	if worktreePath != "" {
		env = append(env, fmt.Sprintf("PREVIEWCTL_WORKTREE_PATH=%s", worktreePath))
	}
	for name, port := range ports {
		env = append(env, fmt.Sprintf("PREVIEWCTL_PORT_%s=%d",
			strings.ToUpper(strings.ReplaceAll(name, "-", "_")), port))
	}
	// Inject persistent store values as PREVIEWCTL_STORE_{KEY}
	if len(envStore) > 0 && envStore[0] != nil {
		for k, v := range envStore[0] {
			env = append(env, fmt.Sprintf("PREVIEWCTL_STORE_%s=%s",
				strings.ToUpper(strings.ReplaceAll(k, "-", "_")), v))
		}
	}
	return env
}

func (m *Manager) CoreInit(ctx context.Context, svcName string) error {
	svc, ok := m.config.Provisioner.Services[svcName]
	if !ok {
		return fmt.Errorf("unknown provisioner service '%s'", svcName)
	}
	if svc.Init == "" {
		return fmt.Errorf("provisioner service '%s' has no init hook defined", svcName)
	}
	initMsg := fmt.Sprintf("Initialized provisioner service %s", svcName)
	if err := m.stepSimple(ctx, "core_init",
		fmt.Sprintf("Running init hook for %s...", svcName),
		&initMsg,
		func() error {
			m.progress.OnStep(StepEvent{Step: "core_init", Status: StepStreaming, Message: fmt.Sprintf("Running init hook for %s...", svcName)})
			_, err := m.runCoreHook(ctx, svcName, "init", "", nil)
			return err
		}); err != nil {
		return fmt.Errorf("initializing provisioner service %s: %w", svcName, err)
	}
	return nil
}

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
	if err := m.stepSimple(ctx, "core_reset",
		fmt.Sprintf("Running reset hook for %s...", svcName),
		&resetMsg,
		func() error {
			m.progress.OnStep(StepEvent{Step: "core_reset", Status: StepStreaming, Message: fmt.Sprintf("Running reset hook for %s...", svcName)})
			outputs, err := m.runCoreHook(ctx, svcName, "reset", envName, entry.Ports)
			if err != nil {
				return err
			}
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

// ---------- DomainLocalComputeAccess ----------

type DomainLocalComputeAccess struct {
	root   string
	stderr io.Writer
}

func NewDomainLocalComputeAccess(root string) ComputeAccess {
	return &DomainLocalComputeAccess{root: root, stderr: os.Stderr}
}

func (l *DomainLocalComputeAccess) SetStderr(w io.Writer) { l.stderr = w }

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
	cmd.Stderr = l.stderr
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
