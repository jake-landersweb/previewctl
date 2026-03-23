package domain

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// mockTracker records the order of operations across all mocks.
type mockTracker struct {
	calls []string
}

func (t *mockTracker) record(call string) {
	t.calls = append(t.calls, call)
}

// mockComputePort implements ComputePort
type mockComputePort struct {
	tracker *mockTracker
	baseDir string // temp dir for worktrees
}

func (m *mockComputePort) Create(_ context.Context, envName string, branch string) (*ComputeResources, error) {
	m.tracker.record("compute.Create")
	path := filepath.Join(m.baseDir, envName)
	_ = os.MkdirAll(path, 0o755)
	return &ComputeResources{
		WorktreePath: path,
	}, nil
}

func (m *mockComputePort) Start(_ context.Context, envName string, ports PortMap) error {
	m.tracker.record("compute.Start")
	return nil
}

func (m *mockComputePort) Stop(_ context.Context, envName string) error {
	m.tracker.record("compute.Stop")
	return nil
}

func (m *mockComputePort) Destroy(_ context.Context, envName string) error {
	m.tracker.record("compute.Destroy")
	return nil
}

func (m *mockComputePort) IsRunning(_ context.Context, envName string) (bool, error) {
	m.tracker.record("compute.IsRunning")
	return true, nil
}

func (m *mockComputePort) DetectBranch(_ context.Context, _ string) (string, error) {
	return "main", nil
}

// mockNetworkingPort implements NetworkingPort
type mockNetworkingPort struct {
	tracker *mockTracker
}

func (m *mockNetworkingPort) AllocatePorts(envName string) (PortMap, error) {
	m.tracker.record("networking.AllocatePorts")
	return PortMap{"backend": 8042, "web": 3042}, nil
}

func (m *mockNetworkingPort) GetServiceURL(envName string, service string) (string, error) {
	return "http://localhost:8042", nil
}

// mockStatePort implements StatePort
type mockStatePort struct {
	tracker      *mockTracker
	environments map[string]*EnvironmentEntry
}

func newMockStatePort(tracker *mockTracker) *mockStatePort {
	return &mockStatePort{
		tracker:      tracker,
		environments: make(map[string]*EnvironmentEntry),
	}
}

func (m *mockStatePort) Load(_ context.Context) (*State, error) {
	m.tracker.record("state.Load")
	return &State{
		Version:      1,
		Environments: m.environments,
	}, nil
}

func (m *mockStatePort) Save(_ context.Context, state *State) error {
	m.tracker.record("state.Save")
	m.environments = state.Environments
	return nil
}

func (m *mockStatePort) GetEnvironment(_ context.Context, name string) (*EnvironmentEntry, error) {
	m.tracker.record("state.GetEnvironment")
	return m.environments[name], nil
}

func (m *mockStatePort) SetEnvironment(_ context.Context, name string, entry *EnvironmentEntry) error {
	m.tracker.record("state.SetEnvironment")
	m.environments[name] = entry
	return nil
}

func (m *mockStatePort) RemoveEnvironment(_ context.Context, name string) error {
	m.tracker.record("state.RemoveEnvironment")
	delete(m.environments, name)
	return nil
}

// mockProgressReporter captures step events
type mockProgressReporter struct {
	events []StepEvent
}

func (m *mockProgressReporter) OnStep(event StepEvent) {
	m.events = append(m.events, event)
}

func newTestManager(tracker *mockTracker) (*Manager, *mockStatePort, *mockProgressReporter) {
	statePort := newMockStatePort(tracker)
	progress := &mockProgressReporter{}

	// Create a temp dir for manifest writes
	tmpDir, _ := os.MkdirTemp("", "previewctl-test-*")

	mgr := NewManager(ManagerDeps{
		Compute:    &mockComputePort{tracker: tracker, baseDir: filepath.Join(tmpDir, "worktrees")},
		Networking: &mockNetworkingPort{tracker: tracker},
		State:      statePort,
		Progress:   progress,
		Config: &ProjectConfig{
			Name: "myproject",
			Services: map[string]ServiceConfig{
				"backend": {Path: "apps/backend"},
				"web":     {Path: "apps/web"},
			},
		},
		ProjectRoot: tmpDir,
	})

	return mgr, statePort, progress
}

func TestManager_Init_CallOrder(t *testing.T) {
	tracker := &mockTracker{}
	mgr, _, _ := newTestManager(tracker)
	ctx := context.Background()

	entry, err := mgr.Init(ctx, "feat-auth", "feat-auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify key operations happened in order
	expectedOps := map[string]bool{
		"compute.Create":           false,
		"networking.AllocatePorts": false,
		"compute.Start":            false,
		"state.SetEnvironment":     false,
	}
	for _, call := range tracker.calls {
		if _, ok := expectedOps[call]; ok {
			expectedOps[call] = true
		}
	}
	for op, found := range expectedOps {
		if !found {
			t.Errorf("expected operation '%s' to be called", op)
		}
	}

	if entry.Name != "feat-auth" {
		t.Errorf("expected name 'feat-auth', got '%s'", entry.Name)
	}
	if entry.Mode != ModeLocal {
		t.Errorf("expected mode 'local', got '%s'", entry.Mode)
	}
	if entry.Status != StatusRunning {
		t.Errorf("expected status 'running', got '%s'", entry.Status)
	}
	if entry.Compute == nil {
		t.Fatal("expected Compute to be set")
	}
	if entry.Compute.Type != "local" {
		t.Errorf("expected compute type 'local', got '%s'", entry.Compute.Type)
	}
}

func TestManager_Init_WritesManifest(t *testing.T) {
	tracker := &mockTracker{}
	mgr, _, _ := newTestManager(tracker)
	ctx := context.Background()

	entry, err := mgr.Init(ctx, "feat-auth", "feat-auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify .previewctl.json was written to the worktree
	manifestPath := filepath.Join(entry.WorktreePath(), ".previewctl.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}

	manifest, err := ReadManifest(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parsing manifest: %v", err)
	}

	if manifest.EnvName != "feat-auth" {
		t.Errorf("expected env_name 'feat-auth', got '%s'", manifest.EnvName)
	}
	if manifest.ProjectName != "myproject" {
		t.Errorf("expected project_name 'myproject', got '%s'", manifest.ProjectName)
	}
	if manifest.Mode != "local" {
		t.Errorf("expected mode 'local', got '%s'", manifest.Mode)
	}
}

func TestManager_Destroy_CallOrder(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManager(tracker)
	ctx := context.Background()

	// Pre-populate state
	statePort.environments["feat-auth"] = &EnvironmentEntry{
		Name: "feat-auth",
		Compute: &ComputeAccessInfo{
			Type:            "local",
			Path:            "/worktrees/feat-auth",
			ManagedWorktree: true,
		},
	}

	// Reset tracker to only capture destroy calls
	tracker.calls = nil

	err := mgr.Destroy(ctx, "feat-auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedOps := map[string]bool{
		"state.GetEnvironment":    false,
		"compute.Destroy":         false,
		"state.RemoveEnvironment": false,
	}
	for _, call := range tracker.calls {
		if _, ok := expectedOps[call]; ok {
			expectedOps[call] = true
		}
	}
	for op, found := range expectedOps {
		if !found {
			t.Errorf("expected operation '%s' to be called", op)
		}
	}
}

func TestManager_Destroy_AttachedWorktree(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManager(tracker)
	ctx := context.Background()

	// Pre-populate state with an unmanaged (attached) worktree
	statePort.environments["attached-env"] = &EnvironmentEntry{
		Name: "attached-env",
		Compute: &ComputeAccessInfo{
			Type:            "local",
			Path:            "/external/worktrees/attached-env",
			ManagedWorktree: false,
		},
	}

	tracker.calls = nil

	err := mgr.Destroy(ctx, "attached-env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should call Stop (not Destroy) for unmanaged worktrees
	hasStop := false
	hasDestroy := false
	for _, call := range tracker.calls {
		if call == "compute.Stop" {
			hasStop = true
		}
		if call == "compute.Destroy" {
			hasDestroy = true
		}
	}
	if !hasStop {
		t.Error("expected compute.Stop for attached worktree")
	}
	if hasDestroy {
		t.Error("should not call compute.Destroy for attached worktree")
	}
}

func TestManager_Destroy_NotFound(t *testing.T) {
	tracker := &mockTracker{}
	mgr, _, _ := newTestManager(tracker)
	ctx := context.Background()

	err := mgr.Destroy(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent environment")
	}
}

func TestManager_List(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManager(tracker)
	ctx := context.Background()

	statePort.environments["env1"] = &EnvironmentEntry{Name: "env1"}
	statePort.environments["env2"] = &EnvironmentEntry{Name: "env2"}

	entries, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestManager_Status(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManager(tracker)
	ctx := context.Background()

	statePort.environments["feat-auth"] = &EnvironmentEntry{Name: "feat-auth"}

	detail, err := mgr.Status(ctx, "feat-auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !detail.InfraRunning {
		t.Error("expected infra running")
	}
}

func TestManager_Init_EmitsProgressEvents(t *testing.T) {
	tracker := &mockTracker{}
	mgr, _, progress := newTestManager(tracker)
	ctx := context.Background()

	_, err := mgr.Init(ctx, "feat-auth", "feat-auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have started/completed pairs for each step
	if len(progress.events) == 0 {
		t.Fatal("expected progress events")
	}

	// Verify first event is create_compute started
	if progress.events[0].Step != "create_compute" || progress.events[0].Status != StepStarted {
		t.Errorf("expected first event to be create_compute/started, got %s/%s",
			progress.events[0].Step, progress.events[0].Status)
	}

	// Verify last event is save_state completed
	last := progress.events[len(progress.events)-1]
	if last.Step != "save_state" || last.Status != StepCompleted {
		t.Errorf("expected last event to be save_state/completed, got %s/%s", last.Step, last.Status)
	}

	// Count started/completed pairs
	started := 0
	completed := 0
	for _, e := range progress.events {
		switch e.Status {
		case StepStarted:
			started++
		case StepCompleted:
			completed++
		}
	}
	if started != completed {
		t.Errorf("mismatched started (%d) and completed (%d) events", started, completed)
	}
}

func newTestManagerWithCoreServices(t *testing.T, tracker *mockTracker) (*Manager, *mockStatePort, *mockProgressReporter) {
	t.Helper()
	statePort := newMockStatePort(tracker)
	progress := &mockProgressReporter{}

	mgr := NewManager(ManagerDeps{
		Compute:     &mockComputePort{tracker: tracker, baseDir: filepath.Join(t.TempDir(), "worktrees")},
		Networking:  &mockNetworkingPort{tracker: tracker},
		State:       statePort,
		Progress:    progress,
		ProjectRoot: t.TempDir(),
		Config: &ProjectConfig{
			Name: "myproject",
			Provisioner: ProvisionerConfig{
				Services: map[string]ProvisionerServiceConfig{
					"postgres": {
						Outputs: []string{"CONNECTION_STRING", "DB_HOST"},
						Init:    `echo "CONNECTION_STRING=postgresql://localhost:5432/db"; echo "DB_HOST=localhost"`,
						Seed:    `echo "CONNECTION_STRING=postgresql://localhost:5432/wt_${PREVIEWCTL_ENV_NAME}"; echo "DB_HOST=localhost"`,
						Reset:   `echo "CONNECTION_STRING=postgresql://localhost:5432/wt_${PREVIEWCTL_ENV_NAME}"; echo "DB_HOST=localhost"`,
						Destroy: `echo "destroyed" >&2`,
					},
				},
			},
		},
	})

	return mgr, statePort, progress
}

func TestManager_CoreInit(t *testing.T) {
	tracker := &mockTracker{}
	mgr, _, _ := newTestManagerWithCoreServices(t, tracker)
	ctx := context.Background()

	err := mgr.CoreInit(ctx, "postgres")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManager_CoreInit_UnknownService(t *testing.T) {
	tracker := &mockTracker{}
	mgr, _, _ := newTestManagerWithCoreServices(t, tracker)
	ctx := context.Background()

	err := mgr.CoreInit(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown core service")
	}
}

func TestManager_CoreReset(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManagerWithCoreServices(t, tracker)
	ctx := context.Background()

	// Pre-populate state
	statePort.environments["feat-auth"] = &EnvironmentEntry{
		Name:  "feat-auth",
		Ports: PortMap{"backend": 61000},
		ProvisionerOutputs: map[string]map[string]string{
			"postgres": {"CONNECTION_STRING": "old", "DB_HOST": "old"},
		},
	}

	err := mgr.CoreReset(ctx, "postgres", "feat-auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify outputs were updated in state
	entry := statePort.environments["feat-auth"]
	if entry.ProvisionerOutputs["postgres"]["DB_HOST"] != "localhost" {
		t.Errorf("expected updated DB_HOST, got '%s'", entry.ProvisionerOutputs["postgres"]["DB_HOST"])
	}
}

func TestManager_CoreReset_MissingEnv(t *testing.T) {
	tracker := &mockTracker{}
	mgr, _, _ := newTestManagerWithCoreServices(t, tracker)
	ctx := context.Background()

	err := mgr.CoreReset(ctx, "postgres", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent environment")
	}
}

func TestManager_Init_WithCoreServices(t *testing.T) {
	tracker := &mockTracker{}
	mgr, _, _ := newTestManagerWithCoreServices(t, tracker)
	ctx := context.Background()

	entry, err := mgr.Init(ctx, "feat-db", "feat-db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify core outputs were captured and stored
	if entry.ProvisionerOutputs == nil {
		t.Fatal("expected ProvisionerOutputs to be populated")
	}
	pgOutputs, ok := entry.ProvisionerOutputs["postgres"]
	if !ok {
		t.Fatal("expected postgres outputs")
	}
	if pgOutputs["DB_HOST"] != "localhost" {
		t.Errorf("expected DB_HOST=localhost, got '%s'", pgOutputs["DB_HOST"])
	}
	if pgOutputs["CONNECTION_STRING"] == "" {
		t.Error("expected CONNECTION_STRING to be set")
	}
}

func TestManager_Destroy_WithCoreServices(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManagerWithCoreServices(t, tracker)
	ctx := context.Background()

	statePort.environments["feat-db"] = &EnvironmentEntry{
		Name:  "feat-db",
		Ports: PortMap{"backend": 61000},
		Compute: &ComputeAccessInfo{
			Type:            "local",
			Path:            "/tmp/worktrees/feat-db",
			ManagedWorktree: true,
		},
		ProvisionerOutputs: map[string]map[string]string{
			"postgres": {"CONNECTION_STRING": "test"},
		},
	}

	tracker.calls = nil
	err := mgr.Destroy(ctx, "feat-db")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify state was cleaned up
	if _, ok := statePort.environments["feat-db"]; ok {
		t.Error("expected environment to be removed from state")
	}
}

func TestManager_Provision_SavesProvisionedState(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManager(tracker)
	ctx := context.Background()

	entry, err := mgr.Provision(ctx, "feat-prov", "feat-prov", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.Status != StatusProvisioned {
		t.Errorf("expected status 'provisioned', got '%s'", entry.Status)
	}
	if entry.Compute == nil {
		t.Fatal("expected Compute to be set")
	}
	if entry.Compute.Type != "local" {
		t.Errorf("expected compute type 'local', got '%s'", entry.Compute.Type)
	}

	// Verify state was persisted
	saved := statePort.environments["feat-prov"]
	if saved == nil {
		t.Fatal("expected state to be saved")
	}
	if saved.Status != StatusProvisioned {
		t.Errorf("expected saved status 'provisioned', got '%s'", saved.Status)
	}

	// Verify runner was NOT called (no compute.Start)
	for _, call := range tracker.calls {
		if call == "compute.Start" {
			t.Error("Provision should NOT call compute.Start (runner phase)")
		}
	}
}

func TestManager_Provision_WritesManifest(t *testing.T) {
	tracker := &mockTracker{}
	mgr, _, _ := newTestManager(tracker)
	ctx := context.Background()

	entry, err := mgr.Provision(ctx, "feat-manifest", "feat-manifest", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify .previewctl.json was written
	manifestPath := filepath.Join(entry.WorktreePath(), ".previewctl.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}

	manifest, err := ReadManifest(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parsing manifest: %v", err)
	}

	if manifest.EnvName != "feat-manifest" {
		t.Errorf("expected env_name 'feat-manifest', got '%s'", manifest.EnvName)
	}
	if manifest.Mode != "local" {
		t.Errorf("expected mode 'local', got '%s'", manifest.Mode)
	}
}

func TestManager_Init_CallsProvisionThenRunner(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManager(tracker)
	ctx := context.Background()

	entry, err := mgr.Init(ctx, "feat-full", "feat-full")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Init should result in StatusRunning (not StatusProvisioned)
	if entry.Status != StatusRunning {
		t.Errorf("expected status 'running', got '%s'", entry.Status)
	}

	// Verify compute.Start was called (runner ran)
	hasStart := false
	for _, call := range tracker.calls {
		if call == "compute.Start" {
			hasStart = true
		}
	}
	if !hasStart {
		t.Error("expected compute.Start to be called (runner phase)")
	}

	// State should show running
	saved := statePort.environments["feat-full"]
	if saved == nil {
		t.Fatal("expected state to be saved")
	}
	if saved.Status != StatusRunning {
		t.Errorf("expected saved status 'running', got '%s'", saved.Status)
	}
}

func TestManager_Run_Stateless(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManager(tracker)
	ctx := context.Background()

	// First provision to get a manifest
	entry, err := mgr.Provision(ctx, "feat-run", "feat-run", "")
	if err != nil {
		t.Fatalf("Provision error: %v", err)
	}

	manifestPath := filepath.Join(entry.WorktreePath(), ".previewctl.json")

	// Reset tracker
	tracker.calls = nil

	// Run from manifest
	err = mgr.Run(ctx, manifestPath, "")
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Verify compute.Start was called (runner ran)
	hasStart := false
	for _, call := range tracker.calls {
		if call == "compute.Start" {
			hasStart = true
		}
	}
	if !hasStart {
		t.Error("expected compute.Start to be called")
	}

	// State should still show "provisioned" — Run is stateless
	saved := statePort.environments["feat-run"]
	if saved.Status != StatusProvisioned {
		t.Errorf("expected status to remain 'provisioned', got '%s'", saved.Status)
	}
}

func TestManager_SetStatus(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManager(tracker)
	ctx := context.Background()

	statePort.environments["test-env"] = &EnvironmentEntry{
		Name:   "test-env",
		Status: StatusProvisioned,
	}

	err := mgr.SetStatus(ctx, "test-env", StatusRunning)
	if err != nil {
		t.Fatalf("SetStatus error: %v", err)
	}

	if statePort.environments["test-env"].Status != StatusRunning {
		t.Errorf("expected status 'running', got '%s'", statePort.environments["test-env"].Status)
	}
}

func TestComputeAccessInfo_Local(t *testing.T) {
	ca := NewDomainLocalComputeAccess("/some/path")
	info := computeAccessInfo(ca, true)

	if info.Type != "local" {
		t.Errorf("expected type 'local', got '%s'", info.Type)
	}
	if info.Path != "/some/path" {
		t.Errorf("expected path '/some/path', got '%s'", info.Path)
	}
	if !info.ManagedWorktree {
		t.Error("expected ManagedWorktree=true")
	}
}

func TestComputeAccessInfo_SSH(t *testing.T) {
	ca := NewDomainSSHComputeAccess("1.2.3.4", "deploy", "/app")
	info := computeAccessInfo(ca, false)

	if info.Type != "ssh" {
		t.Errorf("expected type 'ssh', got '%s'", info.Type)
	}
	if info.Host != "1.2.3.4" {
		t.Errorf("expected host '1.2.3.4', got '%s'", info.Host)
	}
	if info.User != "deploy" {
		t.Errorf("expected user 'deploy', got '%s'", info.User)
	}
	if info.Path != "/app" {
		t.Errorf("expected path '/app', got '%s'", info.Path)
	}
	if info.ManagedWorktree {
		t.Error("expected ManagedWorktree=false")
	}
}

// ---------- Checkpoint tests ----------

func TestManager_Provision_RecordsSteps(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManager(tracker)
	ctx := context.Background()

	_, err := mgr.Provision(ctx, "feat-steps", "feat-steps", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry := statePort.environments["feat-steps"]
	if entry.Steps == nil {
		t.Fatal("expected Steps to be populated")
	}

	// Check key steps were recorded
	for _, stepName := range []string{"create_compute", "allocate_ports", "build_manifest", "write_manifest"} {
		if !entry.StepCompleted(stepName) {
			t.Errorf("expected step '%s' to be completed", stepName)
		}
	}

	// Check audit log has entries
	if len(entry.AuditLog) == 0 {
		t.Error("expected audit log entries")
	}
}

func TestManager_Provision_IdempotentRerun(t *testing.T) {
	tracker := &mockTracker{}
	mgr, _, _ := newTestManager(tracker)
	ctx := context.Background()

	// First provision
	_, err := mgr.Provision(ctx, "feat-idem", "feat-idem", "")
	if err != nil {
		t.Fatalf("first provision error: %v", err)
	}

	// Count compute.Create calls
	createCount1 := 0
	for _, call := range tracker.calls {
		if call == "compute.Create" {
			createCount1++
		}
	}

	// Second provision — should skip completed steps
	tracker.calls = nil
	_, err = mgr.Provision(ctx, "feat-idem", "feat-idem", "")
	if err != nil {
		t.Fatalf("second provision error: %v", err)
	}

	// compute.Create should NOT be called again (cached + verified)
	createCount2 := 0
	for _, call := range tracker.calls {
		if call == "compute.Create" {
			createCount2++
		}
	}
	if createCount2 > 0 {
		t.Errorf("expected compute.Create to be skipped on re-run, but was called %d times", createCount2)
	}
}

func TestManager_Provision_FromStep(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManager(tracker)
	ctx := context.Background()

	// First provision
	_, err := mgr.Provision(ctx, "feat-from", "feat-from", "")
	if err != nil {
		t.Fatalf("first provision error: %v", err)
	}

	// Verify allocate_ports is completed
	if !statePort.environments["feat-from"].StepCompleted("allocate_ports") {
		t.Fatal("expected allocate_ports to be completed")
	}

	// Re-provision with --from allocate_ports
	tracker.calls = nil
	_, err = mgr.Provision(ctx, "feat-from", "feat-from", "allocate_ports")
	if err != nil {
		t.Fatalf("second provision error: %v", err)
	}

	// allocate_ports should have been re-executed (verify via tracker)
	hasAllocate := false
	for _, call := range tracker.calls {
		if call == "networking.AllocatePorts" {
			hasAllocate = true
		}
	}
	if !hasAllocate {
		t.Error("expected allocate_ports to be re-executed after --from")
	}
}

func TestManager_Provision_NoCache(t *testing.T) {
	tracker := &mockTracker{}
	mgr, _, _ := newTestManager(tracker)
	ctx := context.Background()

	// First provision
	_, err := mgr.Provision(ctx, "feat-nocache", "feat-nocache", "")
	if err != nil {
		t.Fatalf("first provision error: %v", err)
	}

	// Second provision with noCache
	tracker.calls = nil
	mgr.SetNoCache(true)
	_, err = mgr.Provision(ctx, "feat-nocache", "feat-nocache", "")
	if err != nil {
		t.Fatalf("second provision error: %v", err)
	}

	// compute.Create should be called again
	hasCreate := false
	for _, call := range tracker.calls {
		if call == "compute.Create" {
			hasCreate = true
		}
	}
	if !hasCreate {
		t.Error("expected compute.Create to be called with --no-cache")
	}
}

func TestManager_Provision_AuditLog(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManager(tracker)
	ctx := context.Background()

	_, err := mgr.Provision(ctx, "feat-audit", "feat-audit", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry := statePort.environments["feat-audit"]
	if len(entry.AuditLog) == 0 {
		t.Fatal("expected audit log entries")
	}

	// Verify audit entries have machine and action
	for _, a := range entry.AuditLog {
		if a.Machine == "" {
			t.Error("expected Machine to be set in audit entry")
		}
		if a.Action == "" {
			t.Error("expected Action to be set in audit entry")
		}
		if a.Step == "" {
			t.Error("expected Step to be set in audit entry")
		}
	}

	// First entry should be "executed" for create_compute
	found := false
	for _, a := range entry.AuditLog {
		if a.Step == "create_compute" && a.Action == "executed" {
			found = true
			if a.DurationMs < 0 {
				t.Error("expected non-negative duration")
			}
			break
		}
	}
	if !found {
		t.Error("expected audit entry for create_compute/executed")
	}
}

func TestManager_StepRecord_Outputs(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManager(tracker)
	ctx := context.Background()

	_, err := mgr.Provision(ctx, "feat-outputs", "feat-outputs", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entry := statePort.environments["feat-outputs"]

	// Check allocate_ports has outputs
	outputs := entry.StepOutputs("allocate_ports")
	if outputs == nil {
		t.Fatal("expected allocate_ports to have outputs")
	}
	if outputs["ports"] == nil {
		t.Error("expected ports in allocate_ports outputs")
	}
}

func TestEnvironmentEntry_InvalidateStepsFrom(t *testing.T) {
	entry := &EnvironmentEntry{
		Steps: map[string]*StepRecord{
			"step_a": {Name: "step_a", Status: StepRecordCompleted},
			"step_b": {Name: "step_b", Status: StepRecordCompleted},
			"step_c": {Name: "step_c", Status: StepRecordCompleted},
		},
	}

	order := []string{"step_a", "step_b", "step_c"}
	entry.InvalidateStepsFrom("step_b", order)

	if !entry.StepCompleted("step_a") {
		t.Error("step_a should not be invalidated")
	}
	if entry.StepCompleted("step_b") {
		t.Error("step_b should be invalidated")
	}
	if entry.StepCompleted("step_c") {
		t.Error("step_c should be invalidated")
	}

	// Check audit log has invalidation entries
	invalidated := 0
	for _, a := range entry.AuditLog {
		if a.Action == "invalidated" {
			invalidated++
		}
	}
	if invalidated != 2 {
		t.Errorf("expected 2 invalidation audit entries, got %d", invalidated)
	}
}

func TestManager_Init_RecordsStepsAndRunning(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManager(tracker)
	ctx := context.Background()

	entry, err := mgr.Init(ctx, "feat-init-steps", "feat-init-steps")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.Status != StatusRunning {
		t.Errorf("expected status 'running', got '%s'", entry.Status)
	}

	saved := statePort.environments["feat-init-steps"]
	if saved.Steps == nil {
		t.Fatal("expected Steps to be populated")
	}

	// Both provisioner and runner steps should be recorded
	if !saved.StepCompleted("create_compute") {
		t.Error("expected create_compute step")
	}
	if !saved.StepCompleted("start_infra") {
		t.Error("expected start_infra step")
	}
}
