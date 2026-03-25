package cli

import (
	"fmt"
	"os"

	filestate "github.com/jake-landersweb/previewctl/src/outbound/state"
	"github.com/spf13/cobra"
)

func newMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run database migrations for the remote state store",
		Long: `Applies pending schema migrations to the Postgres database used for
remote state storage. Run this once before first use, or after upgrading
previewctl to a version with new migrations.

Requires PREVIEWCTL_STATE_DSN to be set.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dsn := os.Getenv("PREVIEWCTL_STATE_DSN")
			if dsn == "" {
				return fmt.Errorf("PREVIEWCTL_STATE_DSN is required")
			}

			Header("Running state database migrations")

			if err := filestate.RunMigrations(dsn); err != nil {
				return fmt.Errorf("migration failed: %w", err)
			}

			Success("Migrations applied successfully")
			return nil
		},
	}

	return cmd
}
