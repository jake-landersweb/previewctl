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
		Short: "Manage template and environment databases",
	}

	cmd.AddCommand(
		newDbSeedCmd(),
		newDbResetCmd(),
	)

	return cmd
}

func newDbSeedCmd() *cobra.Command {
	var dbName string

	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Populate the shared template database (all new environments clone from this)",
		RunE: func(cmd *cobra.Command, args []string) error {
			progress := NewCLIProgressReporter()
			mgr, cfg, err := buildManager(progress)
			if err != nil {
				return err
			}

			if dbName == "" {
				dbName = "main"
			}

			templateDb := cfg.Core.Databases[dbName].Local.TemplateDb
			Header(fmt.Sprintf("Seeding template %s",
				styleDetail.Render(templateDb)))

			if err := mgr.SeedTemplate(cmd.Context(), dbName); err != nil {
				return err
			}

			Success(fmt.Sprintf("Template %s ready",
				styleDetail.Render(templateDb)))

			return nil
		},
	}

	cmd.Flags().StringVar(&dbName, "db", "main", "Database name from config")

	return cmd
}

func newDbResetCmd() *cobra.Command {
	var dbName string

	cmd := &cobra.Command{
		Use:   "reset [name]",
		Short: "Drop and re-clone an environment's database from the template",
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

			templateDb := cfg.Core.Databases[dbName].Local.TemplateDb
			Header(fmt.Sprintf("Resetting %s database for %s",
				styleDetail.Render(dbName),
				styleDetail.Render(envName)))

			if err := mgr.ResetDatabase(cmd.Context(), envName, dbName); err != nil {
				return err
			}

			Success(fmt.Sprintf("Database re-cloned from %s",
				styleDetail.Render(templateDb)))

			return nil
		},
	}

	cmd.Flags().StringVar(&dbName, "db", "main", "Database name from config")

	return cmd
}
