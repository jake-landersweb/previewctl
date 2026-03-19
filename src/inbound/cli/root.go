package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jake-landersweb/previewctl/src/domain"
	"github.com/jake-landersweb/previewctl/src/outbound/local"
	s3adapter "github.com/jake-landersweb/previewctl/src/outbound/s3"
	filestate "github.com/jake-landersweb/previewctl/src/outbound/state"
	"github.com/jake-landersweb/previewctl/src/version"
	"github.com/spf13/cobra"
)

const configFileName = "previewctl.yaml"

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

	rootCmd.AddCommand(
		newCreateCmd(),
		newDeleteCmd(),
		newListCmd(),
		newStatusCmd(),
		newDbCmd(),
		newVetCmd(),
		newCleanCmd(),
		newVersionCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		version.CheckForUpdate()
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
		if dbCfg.Local == nil {
			return nil, nil, fmt.Errorf("database '%s' has no local config", name)
		}
		switch dbCfg.Engine {
		case "postgres":
			databases[name] = local.NewPostgresAdapter(name, *dbCfg.Local)
		default:
			return nil, nil, fmt.Errorf("unsupported database engine '%s' for '%s'", dbCfg.Engine, name)
		}
	}

	// Resolve infrastructure compose file and parse services
	composeFile := ""
	if cfg.Infrastructure != nil && cfg.Infrastructure.ComposeFile != "" {
		composeFile = filepath.Join(projectRoot, cfg.Infrastructure.ComposeFile)
		if _, err := os.Stat(composeFile); os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("infrastructure compose file not found: %s", composeFile)
		}
		infraServices, err := domain.ParseComposeFile(composeFile)
		if err != nil {
			return nil, nil, fmt.Errorf("parsing infrastructure compose file: %w", err)
		}
		cfg.InfraServices = infraServices
	}

	// Build state path
	home, _ := os.UserHomeDir()
	statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")

	// Build seed resolver — use real S3 downloader if any database has S3 seed config
	var s3dl domain.S3Downloader = s3adapter.NoopDownloader{}
	for _, dbCfg := range cfg.Core.Databases {
		if dbCfg.Local != nil {
			for _, step := range dbCfg.Local.Seed {
				if step.S3 != nil {
					s3dl = s3adapter.NewDownloader()
					break
				}
			}
		}
	}

	mgr := domain.NewManager(domain.ManagerDeps{
		Databases:    databases,
		Compute:      local.NewComputeAdapter(cfg, composeFile),
		Networking:   local.NewNetworkingAdapter(cfg),
		EnvGen:       local.NewEnvGenAdapter(cfg),
		State:        filestate.NewFileStateAdapter(statePath),
		Progress:     progress,
		Config:       cfg,
		ProjectRoot:  projectRoot,
		SeedResolver: domain.NewSeedResolver(s3dl),
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
	fullState, err := state.Load(context.TODO())
	if err != nil {
		return "", fmt.Errorf("loading state: %w", err)
	}

	return domain.ResolveEnvironmentFromCwd(cwd, fullState.Environments)
}
