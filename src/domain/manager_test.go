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
	tracker  *mockTracker
	baseDir  string // temp dir for worktrees
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
		"compute.Create":        false,
		"networking.AllocatePorts": false,
		"compute.Start":         false,
		"state.SetEnvironment":  false,
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
		"compute.Destroy":        false,
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

