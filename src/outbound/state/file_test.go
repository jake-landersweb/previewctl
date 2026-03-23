package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jake-landersweb/previewctl/src/domain"
)

func tempStatePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "state.json")
}

func TestFileStateAdapter_LoadEmpty(t *testing.T) {
	adapter := NewFileStateAdapter(tempStatePath(t))
	ctx := context.Background()

	state, err := adapter.Load(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Version != 1 {
		t.Errorf("expected version 1, got %d", state.Version)
	}
	if len(state.Environments) != 0 {
		t.Errorf("expected 0 environments, got %d", len(state.Environments))
	}
}

func TestFileStateAdapter_RoundTrip(t *testing.T) {
	adapter := NewFileStateAdapter(tempStatePath(t))
	ctx := context.Background()

	now := time.Now()
	entry := &domain.EnvironmentEntry{
		Name:        "feat-auth",
		Mode:        domain.ModeLocal,
		Branch:      "feat-auth",
		Status:      domain.StatusRunning,
		CreatedAt:   now,
		UpdatedAt:   now,
		Ports:       domain.PortMap{"backend": 8042, "web": 3042},
		ProvisionerOutputs: make(map[string]map[string]string),
		Local: &domain.LocalMeta{
			WorktreePath:       "/Users/jake/worktrees/myproject/feat-auth",
			ComposeProjectName: "myproject-feat-auth",
		},
	}

	if err := adapter.SetEnvironment(ctx, "feat-auth", entry); err != nil {
		t.Fatalf("SetEnvironment error: %v", err)
	}

	loaded, err := adapter.GetEnvironment(ctx, "feat-auth")
	if err != nil {
		t.Fatalf("GetEnvironment error: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil entry")
	}
	if loaded.Name != "feat-auth" {
		t.Errorf("expected name 'feat-auth', got '%s'", loaded.Name)
	}
	if loaded.Ports["backend"] != 8042 {
		t.Errorf("expected backend port 8042, got %d", loaded.Ports["backend"])
	}
	if loaded.Local.WorktreePath != "/Users/jake/worktrees/myproject/feat-auth" {
		t.Errorf("unexpected worktree path: %s", loaded.Local.WorktreePath)
	}
}

func TestFileStateAdapter_GetNonExistent(t *testing.T) {
	adapter := NewFileStateAdapter(tempStatePath(t))
	ctx := context.Background()

	entry, err := adapter.GetEnvironment(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry != nil {
		t.Error("expected nil entry for nonexistent environment")
	}
}

func TestFileStateAdapter_Remove(t *testing.T) {
	adapter := NewFileStateAdapter(tempStatePath(t))
	ctx := context.Background()

	entry := &domain.EnvironmentEntry{Name: "test", Status: domain.StatusRunning}
	if err := adapter.SetEnvironment(ctx, "test", entry); err != nil {
		t.Fatalf("SetEnvironment error: %v", err)
	}

	if err := adapter.RemoveEnvironment(ctx, "test"); err != nil {
		t.Fatalf("RemoveEnvironment error: %v", err)
	}

	loaded, err := adapter.GetEnvironment(ctx, "test")
	if err != nil {
		t.Fatalf("GetEnvironment error: %v", err)
	}
	if loaded != nil {
		t.Error("expected nil after removal")
	}
}

func TestFileStateAdapter_AtomicWrite(t *testing.T) {
	path := tempStatePath(t)
	adapter := NewFileStateAdapter(path)
	ctx := context.Background()

	entry := &domain.EnvironmentEntry{Name: "test", Status: domain.StatusRunning}
	if err := adapter.SetEnvironment(ctx, "test", entry); err != nil {
		t.Fatalf("SetEnvironment error: %v", err)
	}

	// Verify the file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file should exist: %v", err)
	}

	// Verify no temp file is left behind
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful write")
	}
}

func TestFileStateAdapter_MultipleEnvironments(t *testing.T) {
	adapter := NewFileStateAdapter(tempStatePath(t))
	ctx := context.Background()

	env1 := &domain.EnvironmentEntry{Name: "env1", Status: domain.StatusRunning}
	env2 := &domain.EnvironmentEntry{Name: "env2", Status: domain.StatusStopped}

	if err := adapter.SetEnvironment(ctx, "env1", env1); err != nil {
		t.Fatalf("SetEnvironment error: %v", err)
	}
	if err := adapter.SetEnvironment(ctx, "env2", env2); err != nil {
		t.Fatalf("SetEnvironment error: %v", err)
	}

	state, err := adapter.Load(ctx)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(state.Environments) != 2 {
		t.Errorf("expected 2 environments, got %d", len(state.Environments))
	}
}
