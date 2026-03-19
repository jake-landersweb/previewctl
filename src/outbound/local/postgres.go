package local

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	_ "github.com/lib/pq"

	"github.com/jake-landersweb/previewctl/src/domain"
)

// PostgresAdapter implements domain.DatabasePort for local Postgres
// using a shared master container with template database cloning.
type PostgresAdapter struct {
	config domain.DatabaseModeConfig
	name   string // config key name (e.g., "main")
	host   string // defaults to "localhost", configurable for tests
}

// NewPostgresAdapter creates a new local Postgres adapter.
func NewPostgresAdapter(name string, config domain.DatabaseModeConfig) *PostgresAdapter {
	return &PostgresAdapter{
		config: config,
		name:   name,
		host:   "localhost",
	}
}

// NewPostgresAdapterWithHost creates a Postgres adapter with a custom host.
func NewPostgresAdapterWithHost(name string, config domain.DatabaseModeConfig, host string) *PostgresAdapter {
	return &PostgresAdapter{
		config: config,
		name:   name,
		host:   host,
	}
}

func (a *PostgresAdapter) EnsureInfrastructure(ctx context.Context) error {
	db, err := a.connectDB(ctx, "postgres")
	if err != nil {
		return fmt.Errorf("postgres not reachable on %s:%d: %w", a.host, a.config.Port, err)
	}
	defer func() { _ = db.Close() }()

	return db.PingContext(ctx)
}

func (a *PostgresAdapter) PrepareTemplate(ctx context.Context) error {
	db, err := a.connectDB(ctx, "postgres")
	if err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	templateDb := a.config.TemplateDb

	// Unmark template if it exists (ignore errors if db doesn't exist)
	_, _ = db.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE %s IS_TEMPLATE = false", quoteIdent(templateDb)))

	// Terminate connections to template
	_, _ = db.ExecContext(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1", templateDb)

	// Drop existing template
	_, _ = db.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdent(templateDb)))

	// Create empty template database
	_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", quoteIdent(templateDb)))
	if err != nil {
		return fmt.Errorf("creating template db: %w", err)
	}

	return nil
}

func (a *PostgresAdapter) ApplySeedStep(ctx context.Context, step *domain.SeedMaterial, output io.Writer) error {
	templateDb := a.config.TemplateDb

	switch step.Kind {
	case domain.SeedSQL:
		seedSQL, err := os.ReadFile(step.SQLPath)
		if err != nil {
			return fmt.Errorf("reading sql file: %w", err)
		}
		templateConn, err := a.connectDB(ctx, templateDb)
		if err != nil {
			return fmt.Errorf("connecting to template db: %w", err)
		}
		defer func() { _ = templateConn.Close() }()
		if _, err := templateConn.ExecContext(ctx, string(seedSQL)); err != nil {
			return fmt.Errorf("running sql: %w", err)
		}

	case domain.SeedDump:
		cmd := exec.CommandContext(ctx, "pg_restore",
			"--jobs=4", "--no-owner", "--no-acl",
			"-h", a.host, "-p", fmt.Sprintf("%d", a.config.Port), "-U", a.config.User,
			"-d", templateDb,
			step.DumpPath,
		)
		cmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", a.config.Password))
		cmd.Stdout = output
		cmd.Stderr = output
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("pg_restore failed: %w", err)
		}

	case domain.SeedCommand:
		databaseURL := fmt.Sprintf("postgresql://%s:%s@%s:%d/%s",
			a.config.User, a.config.Password, a.host, a.config.Port, templateDb)
		cmd := exec.CommandContext(ctx, "sh", "-c", step.Command)
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("PGHOST=%s", a.host),
			fmt.Sprintf("PGPORT=%d", a.config.Port),
			fmt.Sprintf("PGUSER=%s", a.config.User),
			fmt.Sprintf("PGPASSWORD=%s", a.config.Password),
			fmt.Sprintf("PGDATABASE=%s", templateDb),
			fmt.Sprintf("DATABASE_URL=%s", databaseURL),
		)
		cmd.Stdout = output
		cmd.Stderr = output
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("command failed: %w", err)
		}
	}

	return nil
}

func (a *PostgresAdapter) FinalizeTemplate(ctx context.Context) error {
	db, err := a.connectDB(ctx, "postgres")
	if err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE %s IS_TEMPLATE = true", quoteIdent(a.config.TemplateDb)))
	if err != nil {
		return fmt.Errorf("marking template: %w", err)
	}

	return nil
}

func (a *PostgresAdapter) CreateDatabase(ctx context.Context, envName string) (*domain.DatabaseInfo, error) {
	dbName := a.envDbName(envName)

	db, err := a.connectDB(ctx, "postgres")
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s",
		quoteIdent(dbName), quoteIdent(a.config.TemplateDb)))
	if err != nil {
		return nil, fmt.Errorf("creating database from template: %w", err)
	}

	return &domain.DatabaseInfo{
		Host:     a.host,
		Port:     a.config.Port,
		User:     a.config.User,
		Password: a.config.Password,
		Database: dbName,
		ConnectionString: fmt.Sprintf("postgresql://%s:%s@%s:%d/%s",
			a.config.User, a.config.Password, a.host, a.config.Port, dbName),
	}, nil
}

func (a *PostgresAdapter) DestroyDatabase(ctx context.Context, envName string) error {
	dbName := a.envDbName(envName)

	db, err := a.connectDB(ctx, "postgres")
	if err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	// Terminate connections
	_, _ = db.ExecContext(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1", dbName)

	_, err = db.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdent(dbName)))
	if err != nil {
		return fmt.Errorf("dropping database: %w", err)
	}

	return nil
}

func (a *PostgresAdapter) ResetDatabase(ctx context.Context, envName string) (*domain.DatabaseInfo, error) {
	if err := a.DestroyDatabase(ctx, envName); err != nil {
		return nil, err
	}
	return a.CreateDatabase(ctx, envName)
}

func (a *PostgresAdapter) DatabaseExists(ctx context.Context, envName string) (bool, error) {
	dbName := a.envDbName(envName)

	db, err := a.connectDB(ctx, "postgres")
	if err != nil {
		return false, fmt.Errorf("connecting to postgres: %w", err)
	}
	defer func() { _ = db.Close() }()

	var exists bool
	err = db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking database existence: %w", err)
	}

	return exists, nil
}

// envDbName generates the environment-specific database name.
func (a *PostgresAdapter) envDbName(envName string) string {
	safe := strings.ReplaceAll(envName, "-", "_")
	return fmt.Sprintf("wt_%s", safe)
}

// connectDB opens a connection to the specified database.
func (a *PostgresAdapter) connectDB(_ context.Context, database string) (*sql.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		a.host, a.config.Port, a.config.User, a.config.Password, database)
	return sql.Open("postgres", dsn)
}

// quoteIdent quotes a PostgreSQL identifier to prevent injection.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
