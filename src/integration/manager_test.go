//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/lib/pq"

	"github.com/jake-landersweb/previewctl/src/domain"
	"github.com/jake-landersweb/previewctl/src/outbound/local"
	filestate "github.com/jake-landersweb/previewctl/src/outbound/state"
	"github.com/jake-landersweb/previewctl/src/testutil"
)

// stubComputePort is a minimal compute adapter for integration tests.
// It creates a real directory (simulating a worktree) but doesn't use git.
type stubComputePort struct {
	baseDir string
}

func (s *stubComputePort) Create(_ context.Context, envName string, _ string) (*domain.ComputeResources, error) {
	path := filepath.Join(s.baseDir, envName)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, err
	}
	return &domain.ComputeResources{WorktreePath: path}, nil
}

func (s *stubComputePort) Start(_ context.Context, _ string, _ domain.PortMap) error { return nil }
func (s *stubComputePort) Stop(_ context.Context, _ string) error                    { return nil }

func (s *stubComputePort) Destroy(_ context.Context, envName string) error {
	return os.RemoveAll(filepath.Join(s.baseDir, envName))
}

func (s *stubComputePort) IsRunning(_ context.Context, envName string) (bool, error) {
	_, err := os.Stat(filepath.Join(s.baseDir, envName))
	return err == nil, nil
}

// stubEnvPort writes a marker file to verify env generation happened.
type stubEnvPort struct{}

func (s *stubEnvPort) Generate(_ context.Context, envName string, workdir string, ports domain.PortMap, databases map[string]*domain.DatabaseInfo) error {
	marker := filepath.Join(workdir, ".previewctl-env-generated")
	content := fmt.Sprintf("env=%s\n", envName)
	for svc, port := range ports {
		content += fmt.Sprintf("port.%s=%d\n", svc, port)
	}
	for dbName, dbInfo := range databases {
		content += fmt.Sprintf("db.%s=%s\n", dbName, dbInfo.Database)
	}
	return os.WriteFile(marker, []byte(content), 0o644)
}

func (s *stubEnvPort) SymlinkSharedEnvFiles(_ context.Context, _ string) error { return nil }

func (s *stubEnvPort) Cleanup(_ context.Context, workdir string) error {
	os.Remove(filepath.Join(workdir, ".previewctl-env-generated"))
	return nil
}

func setupManager(t *testing.T, pg *testutil.PostgresContainer) (*domain.Manager, string) {
	t.Helper()

	tmpDir := t.TempDir()
	worktreeDir := filepath.Join(tmpDir, "worktrees")
	statePath := filepath.Join(tmpDir, "state.json")

	seedFile := filepath.Join(tmpDir, "seed.sql")
	os.WriteFile(seedFile, []byte(`
		CREATE TABLE products (id SERIAL PRIMARY KEY, name TEXT NOT NULL, price NUMERIC);
		INSERT INTO products (name, price) VALUES ('Widget', 9.99), ('Gadget', 19.99);
	`), 0o644)

	config := &domain.ProjectConfig{
		Version: 1,
		Name:    "test-project",
		Core: domain.CoreConfig{
			Databases: map[string]domain.DatabaseConfig{
				"main": {
					Engine:     "postgres",
					Image:      "postgres:16",
					Port:       pg.Port,
					User:       testutil.TestDBUser,
					Password:   testutil.TestDBPassword,
					TemplateDb: "dev_template",
					Seed: &domain.SeedConfig{
						Strategy: "script",
						Script:   seedFile,
					},
				},
			},
		},
		Services: map[string]domain.ServiceConfig{
			"backend": {Path: "apps/backend", Port: 8000},
			"web":     {Path: "apps/web", Port: 3000},
		},
		Infrastructure: map[string]domain.InfraServiceConfig{
			"redis": {Image: "redis:7-alpine", Port: 6379},
		},
	}

	mgr := domain.NewManager(domain.ManagerDeps{
		Databases: map[string]domain.DatabasePort{
			"main": local.NewPostgresAdapterWithHost("main", config.Core.Databases["main"], pg.Host),
		},
		Compute:    &stubComputePort{baseDir: worktreeDir},
		Networking: local.NewNetworkingAdapter(config),
		EnvGen:     &stubEnvPort{},
		State:      filestate.NewFileStateAdapter(statePath),
		Config:     config,
	})

	return mgr, worktreeDir
}

func connectDB(t *testing.T, pg *testutil.PostgresContainer, dbName string) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		pg.Host, pg.Port, testutil.TestDBUser, testutil.TestDBPassword, dbName)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("connecting to %s: %v", dbName, err)
	}
	return db
}

func TestIntegration_FullLifecycle(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)
	mgr, worktreeDir := setupManager(t, pg)

	// 1. Seed the template database
	if err := mgr.SeedTemplate(ctx, "main", ""); err != nil {
		t.Fatalf("SeedTemplate failed: %v", err)
	}

	// 2. Init an environment
	entry, err := mgr.Init(ctx, "feat-auth", "feat/auth")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if entry.Name != "feat-auth" {
		t.Errorf("expected name 'feat-auth', got '%s'", entry.Name)
	}
	if entry.Branch != "feat/auth" {
		t.Errorf("expected branch 'feat/auth', got '%s'", entry.Branch)
	}
	if entry.Status != domain.StatusRunning {
		t.Errorf("expected status 'running', got '%s'", entry.Status)
	}
	if entry.Databases["main"].Name != "wt_feat_auth" {
		t.Errorf("expected db name 'wt_feat_auth', got '%s'", entry.Databases["main"].Name)
	}

	// Verify worktree directory was created
	if _, err := os.Stat(filepath.Join(worktreeDir, "feat-auth")); err != nil {
		t.Errorf("worktree directory should exist: %v", err)
	}

	// Verify env generation marker exists
	markerPath := filepath.Join(worktreeDir, "feat-auth", ".previewctl-env-generated")
	if _, err := os.Stat(markerPath); err != nil {
		t.Error("env generation marker should exist")
	}

	// Verify actual database was cloned with seed data
	db := connectDB(t, pg, "wt_feat_auth")
	defer db.Close()
	var count int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM products").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 products in cloned db, got %d", count)
	}

	// Verify ports were allocated with offset from base
	if entry.Ports["backend"] <= 8000 {
		t.Errorf("expected backend port > 8000, got %d", entry.Ports["backend"])
	}
	if entry.Ports["web"] <= 3000 {
		t.Errorf("expected web port > 3000, got %d", entry.Ports["web"])
	}
	if entry.Ports["redis"] <= 6379 {
		t.Errorf("expected redis port > 6379, got %d", entry.Ports["redis"])
	}

	// 3. List environments
	entries, err := mgr.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 environment, got %d", len(entries))
	}

	// 4. Status check
	detail, err := mgr.Status(ctx, "feat-auth")
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if !detail.DatabaseExists["main"] {
		t.Error("expected database to exist in status check")
	}

	// 5. Destroy
	if err := mgr.Destroy(ctx, "feat-auth"); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	// Verify worktree directory was removed
	if _, err := os.Stat(filepath.Join(worktreeDir, "feat-auth")); !os.IsNotExist(err) {
		t.Error("worktree directory should be removed after destroy")
	}

	// Verify database was dropped
	mainDB := connectDB(t, pg, "postgres")
	defer mainDB.Close()
	var exists bool
	mainDB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = 'wt_feat_auth')").Scan(&exists)
	if exists {
		t.Error("expected database to be dropped after destroy")
	}

	// Verify state is empty
	entries, _ = mgr.List(ctx)
	if len(entries) != 0 {
		t.Errorf("expected 0 environments after destroy, got %d", len(entries))
	}
}

func TestIntegration_MultipleEnvironments(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)
	mgr, _ := setupManager(t, pg)

	mgr.SeedTemplate(ctx, "main", "")

	envNames := []string{"env-one", "env-two", "env-three"}
	for _, name := range envNames {
		if _, err := mgr.Init(ctx, name, name); err != nil {
			t.Fatalf("Init(%s) failed: %v", name, err)
		}
	}

	entries, _ := mgr.List(ctx)
	if len(entries) != 3 {
		t.Fatalf("expected 3 environments, got %d", len(entries))
	}

	// All databases should exist
	for _, name := range envNames {
		detail, err := mgr.Status(ctx, name)
		if err != nil {
			t.Fatalf("Status(%s) failed: %v", name, err)
		}
		if !detail.DatabaseExists["main"] {
			t.Errorf("expected database to exist for %s", name)
		}
	}

	// Verify all ports have offset from base (> base port)
	for _, entry := range entries {
		if entry.Ports["backend"] <= 8000 {
			t.Errorf("expected backend port > 8000 for %s, got %d", entry.Name, entry.Ports["backend"])
		}
	}

	// Destroy one, verify others unaffected
	mgr.Destroy(ctx, "env-two")

	entries, _ = mgr.List(ctx)
	if len(entries) != 2 {
		t.Errorf("expected 2 environments after destroying one, got %d", len(entries))
	}

	for _, name := range []string{"env-one", "env-three"} {
		detail, _ := mgr.Status(ctx, name)
		if !detail.DatabaseExists["main"] {
			t.Errorf("expected database for %s to still exist", name)
		}
	}
}

func TestIntegration_ResetDatabase(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)
	mgr, _ := setupManager(t, pg)

	mgr.SeedTemplate(ctx, "main", "")
	mgr.Init(ctx, "reset-env", "reset-env")

	// Insert extra data
	db := connectDB(t, pg, "wt_reset_env")
	db.ExecContext(ctx, "INSERT INTO products (name, price) VALUES ('Extra', 99.99)")
	db.Close()

	// Verify 3 rows
	db = connectDB(t, pg, "wt_reset_env")
	var count int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM products").Scan(&count)
	db.Close()
	if count != 3 {
		t.Fatalf("expected 3 rows before reset, got %d", count)
	}

	// Reset
	if err := mgr.ResetDatabase(ctx, "reset-env", "main"); err != nil {
		t.Fatalf("ResetDatabase failed: %v", err)
	}

	// Should be back to 2 rows
	db = connectDB(t, pg, "wt_reset_env")
	defer db.Close()
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM products").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 rows after reset, got %d", count)
	}
}

func TestIntegration_ReseedTemplate(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)
	mgr, _ := setupManager(t, pg)

	mgr.SeedTemplate(ctx, "main", "")
	mgr.Init(ctx, "before-reseed", "before-reseed")

	db := connectDB(t, pg, "wt_before_reseed")
	var count int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM products").Scan(&count)
	db.Close()
	if count != 2 {
		t.Fatalf("expected 2 products from original seed, got %d", count)
	}

	// Reseed (idempotent)
	if err := mgr.SeedTemplate(ctx, "main", ""); err != nil {
		t.Fatalf("reseed failed: %v", err)
	}

	mgr.Init(ctx, "after-reseed", "after-reseed")

	db = connectDB(t, pg, "wt_after_reseed")
	defer db.Close()
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM products").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 products from reseeded template, got %d", count)
	}
}

func TestIntegration_DestroyNonexistent(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)
	mgr, _ := setupManager(t, pg)

	err := mgr.Destroy(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error when destroying nonexistent environment")
	}
}

func TestIntegration_StatusWithSnapshotInfo(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)
	mgr, _ := setupManager(t, pg)

	mgr.SeedTemplate(ctx, "main", "")
	mgr.Init(ctx, "status-test", "status-test")

	detail, err := mgr.Status(ctx, "status-test")
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if !detail.DatabaseExists["main"] {
		t.Error("expected database to exist")
	}
	if detail.Entry.Name != "status-test" {
		t.Errorf("expected entry name 'status-test', got '%s'", detail.Entry.Name)
	}
	if detail.SnapshotInfo["main"] == nil || !detail.SnapshotInfo["main"].TemplateReady {
		t.Error("expected snapshot info to show template ready")
	}
}
