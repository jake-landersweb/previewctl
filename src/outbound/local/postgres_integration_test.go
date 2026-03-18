//go:build integration

package local

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/lib/pq"

	"github.com/jake-landersweb/previewctl/src/domain"
	"github.com/jake-landersweb/previewctl/src/testutil"
)

func newTestPostgresAdapter(t *testing.T, pg *testutil.PostgresContainer) *PostgresAdapter {
	t.Helper()
	return NewPostgresAdapterWithHost("main", domain.DatabaseConfig{
		Engine:     "postgres",
		Image:      "postgres:16",
		Port:       pg.Port,
		User:       testutil.TestDBUser,
		Password:   testutil.TestDBPassword,
		TemplateDb: "dev_template",
	}, pg.Host)
}

func connectTestDB(t *testing.T, pg *testutil.PostgresContainer, dbName string) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		pg.Host, pg.Port, testutil.TestDBUser, testutil.TestDBPassword, dbName)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("connecting to %s: %v", dbName, err)
	}
	return db
}

func TestPostgresAdapter_EnsureInfrastructure(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)

	adapter := newTestPostgresAdapter(t, pg)
	if err := adapter.EnsureInfrastructure(ctx); err != nil {
		t.Fatalf("EnsureInfrastructure failed: %v", err)
	}
}

func TestPostgresAdapter_SeedTemplate_Empty(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)

	adapter := newTestPostgresAdapter(t, pg)

	// Seed with no snapshot — should create an empty template db
	if err := adapter.SeedTemplate(ctx, ""); err != nil {
		t.Fatalf("SeedTemplate failed: %v", err)
	}

	// Verify template database exists and is marked as template
	db := connectTestDB(t, pg, "postgres")
	defer db.Close()

	var isTemplate bool
	err := db.QueryRowContext(ctx,
		"SELECT datistemplate FROM pg_database WHERE datname = 'dev_template'").Scan(&isTemplate)
	if err != nil {
		t.Fatalf("querying template status: %v", err)
	}
	if !isTemplate {
		t.Error("expected dev_template to be marked as template")
	}
}

func TestPostgresAdapter_SeedTemplate_WithSeedScript(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)

	// Write a seed script to a temp file
	seedDir := t.TempDir()
	seedFile := filepath.Join(seedDir, "seed.sql")
	seedSQL := `CREATE TABLE users (id SERIAL PRIMARY KEY, name TEXT NOT NULL);
INSERT INTO users (name) VALUES ('alice'), ('bob');`
	os.WriteFile(seedFile, []byte(seedSQL), 0o644)

	adapter := NewPostgresAdapterWithHost("main", domain.DatabaseConfig{
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
	}, pg.Host)

	if err := adapter.SeedTemplate(ctx, ""); err != nil {
		t.Fatalf("SeedTemplate with script failed: %v", err)
	}

	// Verify seed data exists in the template
	db := connectTestDB(t, pg, "postgres")
	defer db.Close()

	// Unmark template temporarily to connect
	db.ExecContext(ctx, `ALTER DATABASE "dev_template" IS_TEMPLATE = false`)
	defer db.ExecContext(ctx, `ALTER DATABASE "dev_template" IS_TEMPLATE = true`)

	templateDB := connectTestDB(t, pg, "dev_template")
	defer templateDB.Close()

	var count int
	if err := templateDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("querying users: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 users, got %d", count)
	}
}

func TestPostgresAdapter_CreateDatabase_FromTemplate(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)

	// Write seed data
	seedDir := t.TempDir()
	seedFile := filepath.Join(seedDir, "seed.sql")
	seedSQL := `CREATE TABLE items (id SERIAL PRIMARY KEY, title TEXT NOT NULL);
INSERT INTO items (title) VALUES ('item1'), ('item2'), ('item3');`
	os.WriteFile(seedFile, []byte(seedSQL), 0o644)

	adapter := NewPostgresAdapterWithHost("main", domain.DatabaseConfig{
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
	}, pg.Host)

	// Seed template first
	if err := adapter.SeedTemplate(ctx, ""); err != nil {
		t.Fatalf("SeedTemplate failed: %v", err)
	}

	// Create environment database from template
	dbInfo, err := adapter.CreateDatabase(ctx, "feat-auth")
	if err != nil {
		t.Fatalf("CreateDatabase failed: %v", err)
	}

	if dbInfo.Database != "wt_feat_auth" {
		t.Errorf("expected database name 'wt_feat_auth', got '%s'", dbInfo.Database)
	}
	if dbInfo.Host != pg.Host {
		t.Errorf("expected host '%s', got '%s'", pg.Host, dbInfo.Host)
	}

	// Verify cloned data exists
	clonedDB := connectTestDB(t, pg, "wt_feat_auth")
	defer clonedDB.Close()

	var count int
	if err := clonedDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM items").Scan(&count); err != nil {
		t.Fatalf("querying items in cloned db: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 items in cloned db, got %d", count)
	}
}

func TestPostgresAdapter_DatabaseExists(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)
	adapter := newTestPostgresAdapter(t, pg)

	// Seed template
	if err := adapter.SeedTemplate(ctx, ""); err != nil {
		t.Fatalf("SeedTemplate failed: %v", err)
	}

	// Before creating — should not exist
	exists, err := adapter.DatabaseExists(ctx, "test-env")
	if err != nil {
		t.Fatalf("DatabaseExists failed: %v", err)
	}
	if exists {
		t.Error("expected database to not exist before creation")
	}

	// Create
	if _, err := adapter.CreateDatabase(ctx, "test-env"); err != nil {
		t.Fatalf("CreateDatabase failed: %v", err)
	}

	// After creating — should exist
	exists, err = adapter.DatabaseExists(ctx, "test-env")
	if err != nil {
		t.Fatalf("DatabaseExists failed: %v", err)
	}
	if !exists {
		t.Error("expected database to exist after creation")
	}
}

func TestPostgresAdapter_DestroyDatabase(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)
	adapter := newTestPostgresAdapter(t, pg)

	// Seed + create
	adapter.SeedTemplate(ctx, "")
	adapter.CreateDatabase(ctx, "destroy-test")

	// Destroy
	if err := adapter.DestroyDatabase(ctx, "destroy-test"); err != nil {
		t.Fatalf("DestroyDatabase failed: %v", err)
	}

	// Verify gone
	exists, _ := adapter.DatabaseExists(ctx, "destroy-test")
	if exists {
		t.Error("expected database to not exist after destruction")
	}
}

func TestPostgresAdapter_ResetDatabase(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)

	// Seed with data
	seedDir := t.TempDir()
	seedFile := filepath.Join(seedDir, "seed.sql")
	os.WriteFile(seedFile, []byte("CREATE TABLE counters (n INT); INSERT INTO counters VALUES (1);"), 0o644)

	adapter := NewPostgresAdapterWithHost("main", domain.DatabaseConfig{
		Engine:     "postgres",
		Image:      "postgres:16",
		Port:       pg.Port,
		User:       testutil.TestDBUser,
		Password:   testutil.TestDBPassword,
		TemplateDb: "dev_template",
		Seed:       &domain.SeedConfig{Strategy: "script", Script: seedFile},
	}, pg.Host)

	adapter.SeedTemplate(ctx, "")
	adapter.CreateDatabase(ctx, "reset-test")

	// Mutate the cloned database
	db := connectTestDB(t, pg, "wt_reset_test")
	db.ExecContext(ctx, "INSERT INTO counters VALUES (2), (3)")
	db.Close()

	// Reset — should re-clone from template (only original row)
	dbInfo, err := adapter.ResetDatabase(ctx, "reset-test")
	if err != nil {
		t.Fatalf("ResetDatabase failed: %v", err)
	}
	if dbInfo.Database != "wt_reset_test" {
		t.Errorf("expected 'wt_reset_test', got '%s'", dbInfo.Database)
	}

	// Verify reset data
	db = connectTestDB(t, pg, "wt_reset_test")
	defer db.Close()

	var count int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM counters").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 row after reset (template data only), got %d", count)
	}
}

func TestPostgresAdapter_MultipleEnvironments(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)
	adapter := newTestPostgresAdapter(t, pg)

	adapter.SeedTemplate(ctx, "")

	// Create multiple environments
	envs := []string{"env-alpha", "env-beta", "env-gamma"}
	for _, env := range envs {
		if _, err := adapter.CreateDatabase(ctx, env); err != nil {
			t.Fatalf("CreateDatabase(%s) failed: %v", env, err)
		}
	}

	// Verify all exist
	for _, env := range envs {
		exists, err := adapter.DatabaseExists(ctx, env)
		if err != nil {
			t.Fatalf("DatabaseExists(%s) failed: %v", env, err)
		}
		if !exists {
			t.Errorf("expected database for '%s' to exist", env)
		}
	}

	// Destroy one, others should remain
	adapter.DestroyDatabase(ctx, "env-beta")

	exists, _ := adapter.DatabaseExists(ctx, "env-beta")
	if exists {
		t.Error("expected env-beta to be destroyed")
	}
	exists, _ = adapter.DatabaseExists(ctx, "env-alpha")
	if !exists {
		t.Error("expected env-alpha to still exist")
	}
	exists, _ = adapter.DatabaseExists(ctx, "env-gamma")
	if !exists {
		t.Error("expected env-gamma to still exist")
	}
}

func TestPostgresAdapter_SeedTemplate_Idempotent(t *testing.T) {
	ctx := context.Background()
	pg := testutil.StartPostgres(ctx, t)
	adapter := newTestPostgresAdapter(t, pg)

	// Seed twice — second call should re-seed cleanly
	if err := adapter.SeedTemplate(ctx, ""); err != nil {
		t.Fatalf("first SeedTemplate failed: %v", err)
	}
	if err := adapter.SeedTemplate(ctx, ""); err != nil {
		t.Fatalf("second SeedTemplate failed: %v", err)
	}

	// Should still be able to create databases
	if _, err := adapter.CreateDatabase(ctx, "after-reseed"); err != nil {
		t.Fatalf("CreateDatabase after reseed failed: %v", err)
	}
}
