package testutil

import (
	"context"
	"fmt"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	TestDBUser     = "testuser"
	TestDBPassword = "testpass"
	TestDBName     = "testdb"
)

// PostgresContainer wraps a testcontainers Postgres instance.
type PostgresContainer struct {
	Container *postgres.PostgresContainer
	Host      string
	Port      int
}

// StartPostgres spins up a Postgres container for integration tests.
func StartPostgres(ctx context.Context, t *testing.T) *PostgresContainer {
	t.Helper()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase(TestDBName),
		postgres.WithUsername(TestDBUser),
		postgres.WithPassword(TestDBPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2),
		),
	)
	if err != nil {
		t.Fatalf("starting postgres container: %v", err)
	}

	t.Cleanup(func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("warning: terminating postgres container: %v", err)
		}
	})

	host, err := pgContainer.Host(ctx)
	if err != nil {
		t.Fatalf("getting container host: %v", err)
	}

	mappedPort, err := pgContainer.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("getting mapped port: %v", err)
	}

	pc := &PostgresContainer{
		Container: pgContainer,
		Host:      host,
		Port:      mappedPort.Int(),
	}

	t.Logf("Postgres container started at %s:%d", pc.Host, pc.Port)
	return pc
}

// ConnectionString returns a DSN for connecting to the test container.
func (pc *PostgresContainer) ConnectionString(database string) string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		pc.Host, pc.Port, TestDBUser, TestDBPassword, database)
}
