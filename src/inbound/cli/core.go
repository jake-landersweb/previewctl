package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jake-landersweb/previewctl/src/domain"
	"github.com/spf13/cobra"
)


func newCoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "core",
		Short: "Manage core services (databases, etc.)",
		Long:  "Manage core services defined in your previewctl.yaml. Run 'previewctl core <name> --help' to see available actions.",
	}

	// Try to load config and add dynamic subcommands at construction time.
	// This allows cobra to show them in --help. If config isn't found
	// (e.g., running from outside a project), the command still works
	// but shows no subcommands.
	if cfg, _, err := loadConfig(); err == nil {
		addCoreDatabaseCommands(cmd, cfg)
	}

	return cmd
}

// addCoreDatabaseCommands adds a subcommand for each database in the config.
func addCoreDatabaseCommands(parent *cobra.Command, cfg *domain.ProjectConfig) {
	for name, db := range cfg.Core.Databases {
		dbCmd := newCoreDatabaseCmd(name, db)
		parent.AddCommand(dbCmd)
	}
}

func newCoreDatabaseCmd(name string, db domain.DatabaseConfig) *cobra.Command {
	provider := "unknown"
	if db.Local != nil {
		provider = db.Local.Provider
		if provider == "" {
			provider = "docker"
		}
	}

	cmd := &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("Manage %s (%s, %s)", name, db.Engine, provider),
	}

	// Add engine-specific actions
	switch db.Engine {
	case "postgres":
		cmd.AddCommand(
			newCoreSeedCmd(name),
			newCoreResetCmd(name),
		)
	}

	return cmd
}

func newCoreSeedCmd(dbName string) *cobra.Command {
	return &cobra.Command{
		Use:   "seed",
		Short: "Populate the shared template database (all new environments clone from this)",
		RunE: func(cmd *cobra.Command, args []string) error {
			progress := NewCLIProgressReporter()
			mgr, cfg, err := buildManager(progress)
			if err != nil {
				return err
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
}

func newCoreResetCmd(dbName string) *cobra.Command {
	return &cobra.Command{
		Use:   "reset [env]",
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
}
