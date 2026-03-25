package state

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"

	"github.com/jake-landersweb/previewctl/src/domain"
)

//go:embed migrations/*.sql
var migrations embed.FS

// PostgresStateAdapter persists state to a PostgreSQL database.
// Each environment is stored as a JSONB row, scoped by project name.
// Run RunMigrations before first use to ensure the schema is up to date.
type PostgresStateAdapter struct {
	db      *sql.DB
	project string
}

// NewPostgresStateAdapter creates a new Postgres-backed state adapter.
// The dsn should be a valid PostgreSQL connection string.
// The project name scopes state so multiple projects can share one database.
// The caller should run RunMigrations separately (via `previewctl migrate`)
// before using the adapter for the first time.
func NewPostgresStateAdapter(dsn, project string) (*PostgresStateAdapter, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening postgres connection: %w", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &PostgresStateAdapter{db: db, project: project}, nil
}

// RunMigrations applies all pending goose migrations from the embedded SQL files.
// Should be called explicitly via `previewctl migrate` before first use.
func RunMigrations(dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("opening postgres connection: %w", err)
	}
	defer func() { _ = db.Close() }()

	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("setting goose dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}

func (a *PostgresStateAdapter) Load(ctx context.Context) (*domain.State, error) {
	rows, err := a.db.QueryContext(ctx,
		`SELECT name, data FROM environments WHERE project = $1`, a.project)
	if err != nil {
		return nil, fmt.Errorf("querying environments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	state := domain.NewState()
	for rows.Next() {
		var name string
		var data []byte
		if err := rows.Scan(&name, &data); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		var entry domain.EnvironmentEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			return nil, fmt.Errorf("unmarshaling environment '%s': %w", name, err)
		}
		state.Environments[name] = &entry
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	return state, nil
}

func (a *PostgresStateAdapter) Save(ctx context.Context, state *domain.State) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM environments WHERE project = $1`, a.project); err != nil {
		return fmt.Errorf("clearing environments: %w", err)
	}

	for name, entry := range state.Environments {
		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshaling environment '%s': %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO environments (project, name, data, updated_at) VALUES ($1, $2, $3, now())`,
			a.project, name, data); err != nil {
			return fmt.Errorf("inserting environment '%s': %w", name, err)
		}
	}

	return tx.Commit()
}

func (a *PostgresStateAdapter) GetEnvironment(ctx context.Context, name string) (*domain.EnvironmentEntry, error) {
	var data []byte
	err := a.db.QueryRowContext(ctx,
		`SELECT data FROM environments WHERE project = $1 AND name = $2`,
		a.project, name).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying environment '%s': %w", name, err)
	}

	var entry domain.EnvironmentEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("unmarshaling environment '%s': %w", name, err)
	}
	return &entry, nil
}

func (a *PostgresStateAdapter) SetEnvironment(ctx context.Context, name string, entry *domain.EnvironmentEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling environment '%s': %w", name, err)
	}

	_, err = a.db.ExecContext(ctx, `
		INSERT INTO environments (project, name, data, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (project, name)
		DO UPDATE SET data = EXCLUDED.data, updated_at = now()
	`, a.project, name, data)
	if err != nil {
		return fmt.Errorf("upserting environment '%s': %w", name, err)
	}
	return nil
}

func (a *PostgresStateAdapter) RemoveEnvironment(ctx context.Context, name string) error {
	_, err := a.db.ExecContext(ctx,
		`DELETE FROM environments WHERE project = $1 AND name = $2`,
		a.project, name)
	if err != nil {
		return fmt.Errorf("removing environment '%s': %w", name, err)
	}
	return nil
}

// Close closes the underlying database connection.
func (a *PostgresStateAdapter) Close() error {
	return a.db.Close()
}
