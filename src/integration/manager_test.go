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

// stubComputePort creates real directories (simulates worktrees without git).
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

// stubEnvPort writes a marker file with captured outputs.
type stubEnvPort struct{}

func (s *stubEnvPort) Generate(_ context.Context, envName string, workdir string, ports domain.PortMap, coreOutputs map[string]map[string]string) error {
	marker := filepath.Join(workdir, ".previewctl-env-generated")
	content := fmt.Sprintf("env=%s\n", envName)
	for svc, port := range ports {
		content += fmt.Sprintf("port.%s=%d\n", svc, port)
	}
	for svcName, outputs := range coreOutputs {
		for key, val := range outputs {
			content += fmt.Sprintf("core.%s.%s=%s\n", svcName, key, val)
		}
	}
	return os.WriteFile(marker, []byte(content), 0o644)
}

func (s *stubEnvPort) SymlinkSharedEnvFiles(_ context.Context, _ string) error { return nil }

func (s *stubEnvPort) Cleanup(_ context.Context, workdir string) error {
	_ = os.Remove(filepath.Join(workdir, ".previewctl-env-generated"))
	return nil
}

// writeSeedScript creates a shell script that clones a database from a template
// and outputs connection details.
func writeSeedScript(t *testing.T, dir string, host string, port int) string {
	t.Helper()
	path := filepath.Join(dir, "seed-env.sh")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
ENV_DB="wt_$(echo "$PREVIEWCTL_ENV_NAME" | tr '-' '_')"
export PGPASSWORD="%s"
psql -h "%s" -p %d -U "%s" -d postgres -c "DROP DATABASE IF EXISTS \"$ENV_DB\";" >&2
psql -h "%s" -p %d -U "%s" -d postgres -c "CREATE DATABASE \"$ENV_DB\" TEMPLATE \"%s\";" >&2
echo "CONNECTION_STRING=postgresql://%s:%s@%s:%d/$ENV_DB"
echo "DB_HOST=%s"
echo "DB_PORT=%d"
echo "DB_NAME=$ENV_DB"
`,
		testutil.TestDBPassword,
		host, port, testutil.TestDBUser,
		host, port, testutil.TestDBUser, "dev_template",
		testutil.TestDBUser, testutil.TestDBPassword, host, port,
		host, port,
	)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing seed script: %v", err)
	}
	return path
}

// writeDestroyScript creates a shell script that drops an environment database.
func writeDestroyScript(t *testing.T, dir string, host string, port int) string {
	t.Helper()
	path := filepath.Join(dir, "destroy-env.sh")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
ENV_DB="wt_$(echo "$PREVIEWCTL_ENV_NAME" | tr '-' '_')"
export PGPASSWORD="%s"
psql -h "%s" -p %d -U "%s" -d postgres -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$ENV_DB';" >&2 2>/dev/null || true
psql -h "%s" -p %d -U "%s" -d postgres -c "DROP DATABASE IF EXISTS \"$ENV_DB\";" >&2
`,
		testutil.TestDBPassword,
		host, port, testutil.TestDBUser,
		host, port, testutil.TestDBUser,
	)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing destroy script: %v", err)
	}
	return path
}

// writeInitScript creates a shell script that sets up a template database.
func writeInitScript(t *testing.T, dir string, host string, port int) string {
	t.Helper()
	path := filepath.Join(dir, "init-db.sh")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
export PGPASSWORD="%s"
psql -h "%s" -p %d -U "%s" -d postgres -c "DROP DATABASE IF EXISTS \"dev_template\";" >&2
psql -h "%s" -p %d -U "%s" -d postgres -c "CREATE DATABASE \"dev_template\";" >&2
psql -h "%s" -p %d -U "%s" -d dev_template -c "CREATE TABLE IF NOT EXISTS seed_marker (id serial PRIMARY KEY, label text);" >&2
psql -h "%s" -p %d -U "%s" -d dev_template -c "INSERT INTO seed_marker (label) VALUES ('seeded');" >&2
psql -h "%s" -p %d -U "%s" -d postgres -c "ALTER DATABASE \"dev_template\" IS_TEMPLATE = true;" >&2
echo "initialized" >&2
`,
		testutil.TestDBPassword,
		host, port, testutil.TestDBUser,
		host, port, testutil.TestDBUser,
		host, port, testutil.TestDBUser,
		host, port, testutil.TestDBUser,
		host, port, testutil.TestDBUser,
	)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing init script: %v", err)
	}
	return path
}

func connectDB(t *testing.T, host string, port int, dbName string) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, testutil.TestDBUser, testutil.TestDBPassword, dbName)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("connecting to %s: %v", dbName, err)
	}
	return db
}

func dbExists(t *testing.T, host string, port int, dbName string) bool {
	t.Helper()
	db := connectDB(t, host, port, "postgres")
	defer func() { _ = db.Close() }()
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists)
	if err != nil {
		t.Fatalf("checking db existence: %v", err)
	}
	return exists
}

func setupManagerWithPostgres(t *testing.T) (*domain.Manager, string, string, int) {
	t.Helper()

	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)

	tmpDir := t.TempDir()
	worktreeDir := filepath.Join(tmpDir, "worktrees")
	statePath := filepath.Join(tmpDir, "state.json")
	scriptsDir := filepath.Join(tmpDir, "scripts")
	_ = os.MkdirAll(scriptsDir, 0o755)

	initScript := writeInitScript(t, scriptsDir, pg.Host, pg.Port)
	seedScript := writeSeedScript(t, scriptsDir, pg.Host, pg.Port)
	destroyScript := writeDestroyScript(t, scriptsDir, pg.Host, pg.Port)

	config := &domain.ProjectConfig{
		Version: 1,
		Name:    "test-project",
		Core: domain.CoreConfig{
			Services: map[string]domain.CoreServiceConfig{
				"postgres": {
					Outputs: []string{"CONNECTION_STRING", "DB_HOST", "DB_PORT", "DB_NAME"},
					Hooks: &domain.CoreServiceHooks{
						Init:    initScript,
						Seed:    seedScript,
						Reset:   seedScript,
						Destroy: destroyScript,
					},
				},
			},
		},
		Services: map[string]domain.ServiceConfig{
			"backend": {Path: "apps/backend"},
		},
		InfraServices: map[string]domain.InfraService{
			"redis": {Name: "redis", Port: 6379},
		},
	}

	mgr := domain.NewManager(domain.ManagerDeps{
		Compute:     &stubComputePort{baseDir: worktreeDir},
		Networking:  local.NewNetworkingAdapter(config),
		EnvGen:      &stubEnvPort{},
		State:       filestate.NewFileStateAdapter(statePath),
		Config:      config,
		ProjectRoot: tmpDir,
	})

	return mgr, worktreeDir, pg.Host, pg.Port
}

func TestIntegration_CoreInit(t *testing.T) {
	ctx := context.Background()
	mgr, _, host, port := setupManagerWithPostgres(t)

	// Run init hook — creates template database
	if err := mgr.CoreInit(ctx, "postgres"); err != nil {
		t.Fatalf("CoreInit failed: %v", err)
	}

	// Verify template database exists
	if !dbExists(t, host, port, "dev_template") {
		t.Error("expected dev_template database to exist after init")
	}

	// Verify seed data exists in template
	db := connectDB(t, host, port, "dev_template")
	defer func() { _ = db.Close() }()
	var label string
	err := db.QueryRow("SELECT label FROM seed_marker LIMIT 1").Scan(&label)
	if err != nil {
		t.Fatalf("querying seed_marker: %v", err)
	}
	if label != "seeded" {
		t.Errorf("expected label 'seeded', got '%s'", label)
	}
}

func TestIntegration_FullLifecycleWithCoreHooks(t *testing.T) {
	ctx := context.Background()
	mgr, worktreeDir, host, port := setupManagerWithPostgres(t)

	// Init template first
	if err := mgr.CoreInit(ctx, "postgres"); err != nil {
		t.Fatalf("CoreInit failed: %v", err)
	}

	// Create environment — should clone template via seed hook
	entry, err := mgr.Init(ctx, "feat-auth", "feat/auth")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Verify core outputs were captured
	if entry.CoreOutputs == nil {
		t.Fatal("expected CoreOutputs to be populated")
	}
	pgOutputs := entry.CoreOutputs["postgres"]
	if pgOutputs == nil {
		t.Fatal("expected postgres outputs")
	}
	if pgOutputs["DB_NAME"] != "wt_feat_auth" {
		t.Errorf("expected DB_NAME=wt_feat_auth, got '%s'", pgOutputs["DB_NAME"])
	}
	if pgOutputs["CONNECTION_STRING"] == "" {
		t.Error("expected CONNECTION_STRING to be set")
	}

	// Verify the cloned database actually exists
	if !dbExists(t, host, port, "wt_feat_auth") {
		t.Error("expected wt_feat_auth database to exist")
	}

	// Verify seed data was cloned
	db := connectDB(t, host, port, "wt_feat_auth")
	var label string
	err = db.QueryRow("SELECT label FROM seed_marker LIMIT 1").Scan(&label)
	_ = db.Close()
	if err != nil {
		t.Fatalf("querying cloned database: %v", err)
	}
	if label != "seeded" {
		t.Errorf("expected cloned data, got '%s'", label)
	}

	// Verify env generation marker has core outputs
	markerPath := filepath.Join(worktreeDir, "feat-auth", ".previewctl-env-generated")
	markerData, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("reading marker: %v", err)
	}
	markerStr := string(markerData)
	if !containsStr(markerStr, "core.postgres.DB_NAME=wt_feat_auth") {
		t.Errorf("expected core outputs in env marker, got:\n%s", markerStr)
	}

	// Destroy — should drop the cloned database
	if err := mgr.Destroy(ctx, "feat-auth"); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	// Verify cloned database was dropped
	if dbExists(t, host, port, "wt_feat_auth") {
		t.Error("expected wt_feat_auth to be dropped after destroy")
	}

	// Verify template still exists
	if !dbExists(t, host, port, "dev_template") {
		t.Error("expected dev_template to survive environment destroy")
	}
}

func TestIntegration_MultipleEnvironments(t *testing.T) {
	ctx := context.Background()
	mgr, _, host, port := setupManagerWithPostgres(t)

	if err := mgr.CoreInit(ctx, "postgres"); err != nil {
		t.Fatalf("CoreInit failed: %v", err)
	}

	// Create two environments
	_, err := mgr.Init(ctx, "env-one", "env-one")
	if err != nil {
		t.Fatalf("Init env-one failed: %v", err)
	}
	_, err = mgr.Init(ctx, "env-two", "env-two")
	if err != nil {
		t.Fatalf("Init env-two failed: %v", err)
	}

	// Both databases should exist
	if !dbExists(t, host, port, "wt_env_one") {
		t.Error("expected wt_env_one")
	}
	if !dbExists(t, host, port, "wt_env_two") {
		t.Error("expected wt_env_two")
	}

	// Destroy one
	if err := mgr.Destroy(ctx, "env-one"); err != nil {
		t.Fatalf("Destroy env-one failed: %v", err)
	}

	// env-one gone, env-two still there
	if dbExists(t, host, port, "wt_env_one") {
		t.Error("expected wt_env_one to be gone")
	}
	if !dbExists(t, host, port, "wt_env_two") {
		t.Error("expected wt_env_two to still exist")
	}
}

func TestIntegration_CoreReset(t *testing.T) {
	ctx := context.Background()
	mgr, _, host, port := setupManagerWithPostgres(t)

	if err := mgr.CoreInit(ctx, "postgres"); err != nil {
		t.Fatalf("CoreInit failed: %v", err)
	}

	// Create environment
	_, err := mgr.Init(ctx, "reset-test", "reset-test")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Add data to the cloned database
	db := connectDB(t, host, port, "wt_reset_test")
	_, err = db.Exec("INSERT INTO seed_marker (label) VALUES ('env-specific-data')")
	_ = db.Close()
	if err != nil {
		t.Fatalf("inserting test data: %v", err)
	}

	// Reset — should re-clone from template (losing env-specific data)
	if err := mgr.CoreReset(ctx, "postgres", "reset-test"); err != nil {
		t.Fatalf("CoreReset failed: %v", err)
	}

	// Verify: env-specific data should be gone, only seed data remains
	db = connectDB(t, host, port, "wt_reset_test")
	defer func() { _ = db.Close() }()
	var count int
	err = db.QueryRow("SELECT count(*) FROM seed_marker").Scan(&count)
	if err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row after reset (seed data only), got %d", count)
	}
}

func TestIntegration_Status(t *testing.T) {
	ctx := context.Background()
	mgr, _, _, _ := setupManagerWithPostgres(t)

	if err := mgr.CoreInit(ctx, "postgres"); err != nil {
		t.Fatalf("CoreInit failed: %v", err)
	}

	_, err := mgr.Init(ctx, "status-test", "status-test")
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	detail, err := mgr.Status(ctx, "status-test")
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if detail.Entry.Name != "status-test" {
		t.Errorf("expected name 'status-test', got '%s'", detail.Entry.Name)
	}
	if detail.Entry.CoreOutputs["postgres"]["DB_NAME"] != "wt_status_test" {
		t.Errorf("expected stored DB_NAME, got '%s'", detail.Entry.CoreOutputs["postgres"]["DB_NAME"])
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && findStr(s, substr)
}

func findStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
