package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jake-landersweb/previewctl/src/domain"
	"github.com/jake-landersweb/previewctl/src/outbound/local"
	filestate "github.com/jake-landersweb/previewctl/src/outbound/state"
	"github.com/jake-landersweb/previewctl/src/version"
	"github.com/spf13/cobra"
)

const configFileName = "previewctl.yaml"

// globalMode holds the --mode flag value, set as a persistent flag on root.
var globalMode string

// Execute runs the CLI.
func Execute() {
	rootCmd := &cobra.Command{
		Use:   "previewctl",
		Short: "Manage isolated preview and development environments",
		Run: func(cmd *cobra.Command, args []string) {
			if v, _ := cmd.Flags().GetBool("version"); v {
				runVersionCheck()
				return
			}
			_ = cmd.Help()
		},
	}
	rootCmd.Flags().BoolP("version", "v", false, "Print the current version and check for updates")
	rootCmd.PersistentFlags().StringVarP(&globalMode, "mode", "m", "local", "Deployment mode (local, remote)")

	rootCmd.AddCommand(
		newCreateCmd(),
		newAttachCmd(),
		newDeleteCmd(),
		newListCmd(),
		newStatusCmd(),
		newProvisionCmd(),
		newRunCmd(),
		newStepsCmd(),
		newProvisionerCmd(),
		newVetCmd(),
		newCleanCmd(),
		newMigrateCmd(),
		newVersionCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		version.CheckForUpdate()
		os.Exit(1)
	}
}

// buildManager loads config using the global --mode flag, wires adapters, and creates a Manager.
func buildManager(progress domain.ProgressReporter) (*domain.Manager, *domain.ProjectConfig, error) {
	return buildManagerWithMode(progress, globalMode)
}

// buildManagerWithMode loads config with the specified mode overlay, wires adapters, and creates a Manager.
func buildManagerWithMode(progress domain.ProgressReporter, mode string) (*domain.Manager, *domain.ProjectConfig, error) {
	cfg, projectRoot, err := loadConfigWithMode(mode)
	if err != nil {
		return nil, nil, err
	}

	// Resolve infrastructure compose file path (already parsed in loadConfig)
	composeFile := ""
	if cfg.Infrastructure != nil && cfg.Infrastructure.ComposeFile != "" {
		composeFile = filepath.Join(projectRoot, cfg.Infrastructure.ComposeFile)
	}

	// Build state adapter based on mode
	var stateAdapter domain.StatePort
	if mode == "remote" {
		dsn := os.Getenv("PREVIEWCTL_STATE_DSN")
		if dsn == "" {
			return nil, nil, fmt.Errorf("PREVIEWCTL_STATE_DSN is required for remote mode")
		}
		pgAdapter, err := filestate.NewPostgresStateAdapter(dsn, cfg.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("connecting to state database: %w", err)
		}
		stateAdapter = pgAdapter
	} else {
		home, _ := os.UserHomeDir()
		statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")
		stateAdapter = filestate.NewFileStateAdapter(statePath)
	}

	mgr := domain.NewManager(domain.ManagerDeps{
		Compute:     local.NewComputeAdapter(cfg, composeFile),
		Networking:  local.NewNetworkingAdapter(cfg),
		State:       stateAdapter,
		Progress:    progress,
		Config:      cfg,
		ProjectRoot: projectRoot,
	})

	return mgr, cfg, nil
}

// loadConfig searches for previewctl.yaml with "local" mode overlay.
func loadConfig() (*domain.ProjectConfig, string, error) {
	return loadConfigWithMode("local")
}

// loadConfigWithMode searches for previewctl.yaml starting from cwd and walking up,
// loading the specified mode overlay (e.g., previewctl.remote.yaml).
func loadConfigWithMode(mode string) (*domain.ProjectConfig, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("getting cwd: %w", err)
	}

	dir := cwd
	for {
		path := filepath.Join(dir, configFileName)
		if _, err := os.Stat(path); err == nil {
			cfg, err := domain.LoadConfigWithOverlay(path, mode)
			if err != nil {
				return nil, "", err
			}
			// Parse infrastructure compose file to populate InfraServices
			if cfg.Infrastructure != nil && cfg.Infrastructure.ComposeFile != "" {
				composePath := filepath.Join(dir, cfg.Infrastructure.ComposeFile)
				infraServices, err := domain.ParseComposeFile(composePath)
				if err != nil {
					return nil, "", fmt.Errorf("parsing infrastructure compose file: %w", err)
				}
				cfg.InfraServices = infraServices
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
	fullState, err := state.Load(context.TODO())
	if err != nil {
		return "", fmt.Errorf("loading state: %w", err)
	}

	return domain.ResolveEnvironmentFromCwd(cwd, fullState.Environments)
}
