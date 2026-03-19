package domain

import (
	"context"
	"fmt"
	"io"
	"testing"
)

// mockTracker records the order of operations across all mocks.
type mockTracker struct {
	calls []string
}

func (t *mockTracker) record(call string) {
	t.calls = append(t.calls, call)
}

// mockDatabasePort implements DatabasePort
type mockDatabasePort struct {
	tracker *mockTracker
	name    string
}

func (m *mockDatabasePort) EnsureInfrastructure(_ context.Context) error {
	m.tracker.record(fmt.Sprintf("db.%s.EnsureInfrastructure", m.name))
	return nil
}

func (m *mockDatabasePort) PrepareTemplate(_ context.Context) error {
	m.tracker.record(fmt.Sprintf("db.%s.PrepareTemplate", m.name))
	return nil
}

func (m *mockDatabasePort) ApplySeedStep(_ context.Context, _ *SeedMaterial, _ io.Writer) error {
	m.tracker.record(fmt.Sprintf("db.%s.ApplySeedStep", m.name))
	return nil
}

func (m *mockDatabasePort) FinalizeTemplate(_ context.Context) error {
	m.tracker.record(fmt.Sprintf("db.%s.FinalizeTemplate", m.name))
	return nil
}

func (m *mockDatabasePort) CreateDatabase(_ context.Context, envName string) (*DatabaseInfo, error) {
	m.tracker.record(fmt.Sprintf("db.%s.CreateDatabase", m.name))
	return &DatabaseInfo{
		Host:             "localhost",
		Port:             5500,
		User:             "postgres",
		Password:         "postgres",
		Database:         fmt.Sprintf("wt_%s", envName),
		ConnectionString: fmt.Sprintf("postgresql://postgres:postgres@localhost:5500/wt_%s", envName),
	}, nil
}

func (m *mockDatabasePort) DestroyDatabase(_ context.Context, envName string) error {
	m.tracker.record(fmt.Sprintf("db.%s.DestroyDatabase", m.name))
	return nil
}

func (m *mockDatabasePort) ResetDatabase(_ context.Context, envName string) (*DatabaseInfo, error) {
	m.tracker.record(fmt.Sprintf("db.%s.ResetDatabase", m.name))
	return &DatabaseInfo{Database: fmt.Sprintf("wt_%s", envName)}, nil
}

func (m *mockDatabasePort) DatabaseExists(_ context.Context, envName string) (bool, error) {
	m.tracker.record(fmt.Sprintf("db.%s.DatabaseExists", m.name))
	return true, nil
}

// mockComputePort implements ComputePort
type mockComputePort struct {
	tracker *mockTracker
}

func (m *mockComputePort) Create(_ context.Context, envName string, branch string) (*ComputeResources, error) {
	m.tracker.record("compute.Create")
	return &ComputeResources{
		WorktreePath: fmt.Sprintf("/worktrees/%s", envName),
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

// mockNetworkingPort implements NetworkingPort
type mockNetworkingPort struct {
	tracker *mockTracker
}

func (m *mockNetworkingPort) AllocatePorts(envName string) (PortMap, error) {
	m.tracker.record("networking.AllocatePorts")
	return PortMap{"backend": 8042, "web": 3042}, nil
}

func (m *mockNetworkingPort) GetServiceURL(envName string, service string) (string, error) {
	return fmt.Sprintf("http://localhost:%d", 8042), nil
}

// mockEnvPort implements EnvPort
type mockEnvPort struct {
	tracker *mockTracker
}

func (m *mockEnvPort) Generate(_ context.Context, envName string, workdir string, ports PortMap, databases map[string]*DatabaseInfo) error {
	m.tracker.record("env.Generate")
	return nil
}

func (m *mockEnvPort) SymlinkSharedEnvFiles(_ context.Context, workdir string) error {
	m.tracker.record("env.SymlinkSharedEnvFiles")
	return nil
}

func (m *mockEnvPort) Cleanup(_ context.Context, workdir string) error {
	m.tracker.record("env.Cleanup")
	return nil
}

// mockStatePort implements StatePort
type mockStatePort struct {
	tracker      *mockTracker
	environments map[string]*EnvironmentEntry
	snapshots    map[string]*SnapshotState
}

func newMockStatePort(tracker *mockTracker) *mockStatePort {
	return &mockStatePort{
		tracker:      tracker,
		environments: make(map[string]*EnvironmentEntry),
		snapshots:    make(map[string]*SnapshotState),
	}
}

func (m *mockStatePort) Load(_ context.Context) (*State, error) {
	m.tracker.record("state.Load")
	return &State{
		Version:      1,
		Environments: m.environments,
		Snapshots:    m.snapshots,
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

func (m *mockStatePort) UpdateSnapshot(_ context.Context, dbName string, info *SnapshotUpdate) error {
	m.tracker.record("state.UpdateSnapshot")
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

	mgr := NewManager(ManagerDeps{
		Databases: map[string]DatabasePort{
			"main": &mockDatabasePort{tracker: tracker, name: "main"},
		},
		Compute:    &mockComputePort{tracker: tracker},
		Networking: &mockNetworkingPort{tracker: tracker},
		EnvGen:     &mockEnvPort{tracker: tracker},
		State:      statePort,
		Progress:   progress,
		SeedResolver: NewSeedResolver(),
		Config: &ProjectConfig{
			Name: "myproject",
			Core: CoreConfig{
				Databases: map[string]DatabaseConfig{
					"main": {
						Engine: "postgres",
						Local: &DatabaseModeConfig{
							Image:      "postgres:16",
							Port:       5500,
							User:       "postgres",
							Password:   "postgres",
							TemplateDb: "dev_template",
						},
					},
				},
			},
		},
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

	expectedOrder := []string{
		"networking.AllocatePorts",
		"compute.Create",
		"db.main.EnsureInfrastructure",
		"db.main.CreateDatabase",
		"env.SymlinkSharedEnvFiles",
		"env.Generate",
		"compute.Start",
		"state.SetEnvironment",
	}

	if len(tracker.calls) != len(expectedOrder) {
		t.Fatalf("expected %d calls, got %d: %v", len(expectedOrder), len(tracker.calls), tracker.calls)
	}

	for i, expected := range expectedOrder {
		if tracker.calls[i] != expected {
			t.Errorf("call %d: expected '%s', got '%s'", i, expected, tracker.calls[i])
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
	if entry.Local.ComposeProjectName != "myproject-feat-auth" {
		t.Errorf("expected compose name 'myproject-feat-auth', got '%s'", entry.Local.ComposeProjectName)
	}
}

func TestManager_Destroy_CallOrder(t *testing.T) {
	tracker := &mockTracker{}
	mgr, statePort, _ := newTestManager(tracker)
	ctx := context.Background()

	// Pre-populate state
	statePort.environments["feat-auth"] = &EnvironmentEntry{
		Name: "feat-auth",
		Local: &LocalMeta{
			WorktreePath:       "/worktrees/feat-auth",
			ComposeProjectName: "myproject-feat-auth",
		},
	}

	// Reset tracker to only capture destroy calls
	tracker.calls = nil

	err := mgr.Destroy(ctx, "feat-auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedOrder := []string{
		"state.GetEnvironment",
		"compute.Destroy",
		"db.main.DestroyDatabase",
		"env.Cleanup",
		"state.RemoveEnvironment",
	}

	if len(tracker.calls) != len(expectedOrder) {
		t.Fatalf("expected %d calls, got %d: %v", len(expectedOrder), len(tracker.calls), tracker.calls)
	}

	for i, expected := range expectedOrder {
		if tracker.calls[i] != expected {
			t.Errorf("call %d: expected '%s', got '%s'", i, expected, tracker.calls[i])
		}
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
	if !detail.DatabaseExists["main"] {
		t.Error("expected database to exist")
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

	// Verify first event is allocate_ports started
	if progress.events[0].Step != "allocate_ports" || progress.events[0].Status != StepStarted {
		t.Errorf("expected first event to be allocate_ports/started, got %s/%s",
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

func TestManager_SeedTemplate(t *testing.T) {
	tracker := &mockTracker{}
	mgr, _, _ := newTestManager(tracker)
	ctx := context.Background()

	err := mgr.SeedTemplate(ctx, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedCalls := []string{
		"db.main.EnsureInfrastructure",
		"db.main.PrepareTemplate",
		"db.main.FinalizeTemplate",
		"state.UpdateSnapshot",
	}

	if len(tracker.calls) != len(expectedCalls) {
		t.Fatalf("expected %d calls, got %d: %v", len(expectedCalls), len(tracker.calls), tracker.calls)
	}
	for i, expected := range expectedCalls {
		if tracker.calls[i] != expected {
			t.Errorf("call %d: expected '%s', got '%s'", i, expected, tracker.calls[i])
		}
	}
}

func TestManager_ResetDatabase(t *testing.T) {
	tracker := &mockTracker{}
	mgr, _, _ := newTestManager(tracker)
	ctx := context.Background()

	err := mgr.ResetDatabase(ctx, "feat-auth", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedCalls := []string{
		"db.main.DestroyDatabase",
		"db.main.CreateDatabase",
	}
	if len(tracker.calls) != len(expectedCalls) {
		t.Fatalf("expected %d calls, got %d: %v", len(expectedCalls), len(tracker.calls), tracker.calls)
	}
	for i, expected := range expectedCalls {
		if tracker.calls[i] != expected {
			t.Errorf("call %d: expected '%s', got '%s'", i, expected, tracker.calls[i])
		}
	}
}

func TestManager_ResetDatabase_UnknownDB(t *testing.T) {
	tracker := &mockTracker{}
	mgr, _, _ := newTestManager(tracker)
	ctx := context.Background()

	err := mgr.ResetDatabase(ctx, "feat-auth", "unknown")
	if err == nil {
		t.Fatal("expected error for unknown database")
	}
}
