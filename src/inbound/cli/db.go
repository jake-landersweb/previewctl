package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newDbCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database management commands",
	}

	cmd.AddCommand(
		newDbSeedCmd(),
		newDbResetCmd(),
	)

	return cmd
}

func newDbSeedCmd() *cobra.Command {
	var snapshotPath string
	var dbName string

	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Seed or refresh the template database",
		RunE: func(cmd *cobra.Command, args []string) error {
			progress := NewCLIProgressReporter()
			mgr, _, err := buildManager(progress)
			if err != nil {
				return err
			}

			if dbName == "" {
				dbName = "main"
			}

			Header(fmt.Sprintf("Seeding template database %s",
				styleDetail.Render(dbName)))

			if err := mgr.SeedTemplate(cmd.Context(), dbName, snapshotPath); err != nil {
				return err
			}

			Success(fmt.Sprintf("Template database %s seeded",
				styleDetail.Render(dbName)))

			return nil
		},
	}

	cmd.Flags().StringVar(&snapshotPath, "snapshot", "", "Path to database snapshot file")
	cmd.Flags().StringVar(&dbName, "db", "main", "Database name from config")

	return cmd
}

func newDbResetCmd() *cobra.Command {
	var dbName string

	cmd := &cobra.Command{
		Use:   "reset [name]",
		Short: "Reset an environment's database from the template",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			progress := NewCLIProgressReporter()
			mgr, cfg, err := buildManager(progress)
			if err != nil {
				return err
			}

			home, _ := os.UserHomeDir()
			statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")

			envName, err := resolveEnvName(args, statePath)
			if err != nil {
				return fmt.Errorf("could not determine environment: %w", err)
			}

			if dbName == "" {
				dbName = "main"
			}

			Header(fmt.Sprintf("Resetting database %s for %s",
				styleDetail.Render(dbName),
				styleDetail.Render(envName)))

			if err := mgr.ResetDatabase(cmd.Context(), envName, dbName); err != nil {
				return err
			}

			Success(fmt.Sprintf("Database %s reset",
				styleDetail.Render(dbName)))

			return nil
		},
	}

	cmd.Flags().StringVar(&dbName, "db", "main", "Database name from config")

	return cmd
}
