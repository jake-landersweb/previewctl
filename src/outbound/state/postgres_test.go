//go:build integration

package state

import (
	"context"
	"testing"
	"time"

	"github.com/jake-landersweb/previewctl/src/domain"
	"github.com/jake-landersweb/previewctl/src/testutil"
)

func setupPostgresAdapter(t *testing.T) *PostgresStateAdapter {
	t.Helper()
	ctx := context.Background()

	pg := testutil.StartPostgres(ctx, t)
	dsn := pg.ConnectionString(testutil.TestDBName)

	if err := RunMigrations(dsn); err != nil {
		t.Fatalf("running migrations: %v", err)
	}

	adapter, err := NewPostgresStateAdapter(dsn, "testproject")
	if err != nil {
		t.Fatalf("creating adapter: %v", err)
	}
	t.Cleanup(func() { adapter.Close() })

	return adapter
}

func TestPostgresStateAdapter_SetAndGetEnvironment(t *testing.T) {
	adapter := setupPostgresAdapter(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	entry := &domain.EnvironmentEntry{
		Name:      "feat-auth",
		Mode:      domain.ModeLocal,
		Branch:    "feat-auth",
		Status:    domain.StatusRunning,
		CreatedAt: now,
		UpdatedAt: now,
		Ports:     domain.PortMap{"backend": 8042, "web": 3042},
	}

	if err := adapter.SetEnvironment(ctx, "feat-auth", entry); err != nil {
		t.Fatalf("SetEnvironment: %v", err)
	}

	got, err := adapter.GetEnvironment(ctx, "feat-auth")
	if err != nil {
		t.Fatalf("GetEnvironment: %v", err)
	}
	if got == nil {
		t.Fatal("expected entry, got nil")
	}
	if got.Name != "feat-auth" {
		t.Errorf("expected name 'feat-auth', got '%s'", got.Name)
	}
	if got.Ports["backend"] != 8042 {
		t.Errorf("expected backend port 8042, got %d", got.Ports["backend"])
	}
}

func TestPostgresStateAdapter_GetEnvironment_NotFound(t *testing.T) {
	adapter := setupPostgresAdapter(t)
	ctx := context.Background()

	got, err := adapter.GetEnvironment(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestPostgresStateAdapter_RemoveEnvironment(t *testing.T) {
	adapter := setupPostgresAdapter(t)
	ctx := context.Background()

	entry := &domain.EnvironmentEntry{
		Name:   "to-delete",
		Mode:   domain.ModeLocal,
		Branch: "to-delete",
		Status: domain.StatusRunning,
	}

	if err := adapter.SetEnvironment(ctx, "to-delete", entry); err != nil {
		t.Fatalf("SetEnvironment: %v", err)
	}

	if err := adapter.RemoveEnvironment(ctx, "to-delete"); err != nil {
		t.Fatalf("RemoveEnvironment: %v", err)
	}

	got, err := adapter.GetEnvironment(ctx, "to-delete")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after removal, got %v", got)
	}
}

func TestPostgresStateAdapter_Load(t *testing.T) {
	adapter := setupPostgresAdapter(t)
	ctx := context.Background()

	entries := map[string]*domain.EnvironmentEntry{
		"env-a": {Name: "env-a", Mode: domain.ModeLocal, Branch: "a", Status: domain.StatusRunning},
		"env-b": {Name: "env-b", Mode: domain.ModeLocal, Branch: "b", Status: domain.StatusProvisioned},
	}

	for name, entry := range entries {
		if err := adapter.SetEnvironment(ctx, name, entry); err != nil {
			t.Fatalf("SetEnvironment(%s): %v", name, err)
		}
	}

	state, err := adapter.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(state.Environments) != 2 {
		t.Errorf("expected 2 environments, got %d", len(state.Environments))
	}
	if state.Environments["env-a"].Branch != "a" {
		t.Errorf("expected branch 'a', got '%s'", state.Environments["env-a"].Branch)
	}
}

func TestPostgresStateAdapter_Save(t *testing.T) {
	adapter := setupPostgresAdapter(t)
	ctx := context.Background()

	// Seed an initial entry
	if err := adapter.SetEnvironment(ctx, "old", &domain.EnvironmentEntry{
		Name: "old", Mode: domain.ModeLocal, Branch: "old", Status: domain.StatusRunning,
	}); err != nil {
		t.Fatalf("initial SetEnvironment: %v", err)
	}

	// Save replaces all state
	newState := domain.NewState()
	newState.Environments["new-only"] = &domain.EnvironmentEntry{
		Name: "new-only", Mode: domain.ModeLocal, Branch: "new", Status: domain.StatusRunning,
	}

	if err := adapter.Save(ctx, newState); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := adapter.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Environments) != 1 {
		t.Errorf("expected 1 environment after Save, got %d", len(loaded.Environments))
	}
	if _, ok := loaded.Environments["old"]; ok {
		t.Error("expected 'old' to be removed after Save")
	}
	if _, ok := loaded.Environments["new-only"]; !ok {
		t.Error("expected 'new-only' to exist after Save")
	}
}

func TestPostgresStateAdapter_ProjectScoping(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)
	dsn := pg.ConnectionString(testutil.TestDBName)

	if err := RunMigrations(dsn); err != nil {
		t.Fatalf("running migrations: %v", err)
	}

	adapterA, err := NewPostgresStateAdapter(dsn, "project-a")
	if err != nil {
		t.Fatalf("creating adapter A: %v", err)
	}
	defer adapterA.Close()

	adapterB, err := NewPostgresStateAdapter(dsn, "project-b")
	if err != nil {
		t.Fatalf("creating adapter B: %v", err)
	}
	defer adapterB.Close()

	// Set env in project A
	if err := adapterA.SetEnvironment(ctx, "shared-name", &domain.EnvironmentEntry{
		Name: "shared-name", Mode: domain.ModeLocal, Branch: "a", Status: domain.StatusRunning,
	}); err != nil {
		t.Fatalf("SetEnvironment A: %v", err)
	}

	// Project B should not see it
	got, err := adapterB.GetEnvironment(ctx, "shared-name")
	if err != nil {
		t.Fatalf("GetEnvironment B: %v", err)
	}
	if got != nil {
		t.Error("expected project B to not see project A's environment")
	}

	// Project B can have its own entry with the same name
	if err := adapterB.SetEnvironment(ctx, "shared-name", &domain.EnvironmentEntry{
		Name: "shared-name", Mode: domain.ModeLocal, Branch: "b", Status: domain.StatusProvisioned,
	}); err != nil {
		t.Fatalf("SetEnvironment B: %v", err)
	}

	gotA, _ := adapterA.GetEnvironment(ctx, "shared-name")
	gotB, _ := adapterB.GetEnvironment(ctx, "shared-name")
	if gotA.Branch != "a" {
		t.Errorf("project A branch should be 'a', got '%s'", gotA.Branch)
	}
	if gotB.Branch != "b" {
		t.Errorf("project B branch should be 'b', got '%s'", gotB.Branch)
	}
}

func TestPostgresStateAdapter_UpsertOverwrites(t *testing.T) {
	adapter := setupPostgresAdapter(t)
	ctx := context.Background()

	entry := &domain.EnvironmentEntry{
		Name: "env", Mode: domain.ModeLocal, Branch: "main", Status: domain.StatusRunning,
	}
	if err := adapter.SetEnvironment(ctx, "env", entry); err != nil {
		t.Fatalf("first SetEnvironment: %v", err)
	}

	// Update status
	entry.Status = domain.StatusStopped
	if err := adapter.SetEnvironment(ctx, "env", entry); err != nil {
		t.Fatalf("second SetEnvironment: %v", err)
	}

	got, _ := adapter.GetEnvironment(ctx, "env")
	if got.Status != domain.StatusStopped {
		t.Errorf("expected status 'stopped', got '%s'", got.Status)
	}
}
