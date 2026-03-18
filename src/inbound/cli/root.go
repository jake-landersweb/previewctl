package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jake/previewctl/src/domain"
	"github.com/jake/previewctl/src/outbound/local"
	filestate "github.com/jake/previewctl/src/outbound/state"
	"github.com/spf13/cobra"
)

const configFileName = "previewctl.yaml"

// Execute runs the CLI.
func Execute() {
	rootCmd := &cobra.Command{
		Use:   "previewctl",
		Short: "Manage isolated preview and development environments",
	}

	rootCmd.AddCommand(
		newCreateCmd(),
		newDeleteCmd(),
		newListCmd(),
		newStatusCmd(),
		newDbCmd(),
		newVetCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// buildManager loads config, wires adapters, and creates a Manager.
func buildManager(progress domain.ProgressReporter) (*domain.Manager, *domain.ProjectConfig, error) {
	cfg, projectRoot, err := loadConfig()
	if err != nil {
		return nil, nil, err
	}

	// Build database adapters
	databases := make(map[string]domain.DatabasePort)
	for name, dbCfg := range cfg.Core.Databases {
		switch dbCfg.Engine {
		case "postgres":
			databases[name] = local.NewPostgresAdapter(name, dbCfg)
		default:
			return nil, nil, fmt.Errorf("unsupported database engine '%s' for '%s'", dbCfg.Engine, name)
		}
	}

	// Resolve compose file from config (relative to project root)
	composeFile := ""
	if cfg.Local != nil && cfg.Local.ComposeFile != "" {
		composeFile = filepath.Join(projectRoot, cfg.Local.ComposeFile)
		if _, err := os.Stat(composeFile); os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("compose file not found: %s", composeFile)
		}
	} else if len(cfg.Infrastructure) > 0 {
		return nil, nil, fmt.Errorf("infrastructure services defined but 'local.composeFile' is not set in %s", configFileName)
	}

	// Build state path
	home, _ := os.UserHomeDir()
	statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")

	mgr := domain.NewManager(domain.ManagerDeps{
		Databases:  databases,
		Compute:    local.NewComputeAdapter(cfg, composeFile),
		Networking: local.NewNetworkingAdapter(cfg),
		EnvGen:     local.NewEnvGenAdapter(cfg),
		State:      filestate.NewFileStateAdapter(statePath),
		Progress:   progress,
		Config:     cfg,
	})

	return mgr, cfg, nil
}

// loadConfig searches for previewctl.yaml starting from cwd and walking up.
// Returns the config and the directory where it was found (project root).
func loadConfig() (*domain.ProjectConfig, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("getting cwd: %w", err)
	}

	dir := cwd
	for {
		path := filepath.Join(dir, configFileName)
		if _, err := os.Stat(path); err == nil {
			cfg, err := domain.LoadConfig(path)
			if err != nil {
				return nil, "", err
			}
			return cfg, dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, "", fmt.Errorf("could not find %s in %s or any parent directory", configFileName, cwd)
		}
		dir = parent
	}
}

// resolveEnvName resolves an environment name from args or cwd.
func resolveEnvName(args []string, statePath string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}

	// Try to resolve from cwd
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting cwd: %w", err)
	}

	state := filestate.NewFileStateAdapter(statePath)
	fullState, err := state.Load(nil)
	if err != nil {
		return "", fmt.Errorf("loading state: %w", err)
	}

	return domain.ResolveEnvironmentFromCwd(cwd, fullState.Environments)
}
