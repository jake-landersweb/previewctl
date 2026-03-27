package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jake-landersweb/previewctl/src/domain"
	"github.com/jake-landersweb/previewctl/src/outbound/local"
	filestate "github.com/jake-landersweb/previewctl/src/outbound/state"
	"github.com/jake-landersweb/previewctl/src/version"
	"github.com/spf13/cobra"
)

const configFileName = "previewctl.yaml"

// globalMode holds the --mode flag value, set as a persistent flag on root.
// Empty string means "infer from environment state".
var globalMode string

// globalEnvName holds the --env flag value, set as a persistent flag on root.
var globalEnvName string

// globalEnvFiles holds the --env-file flag value.
var globalEnvFiles string

// Execute runs the CLI.
func Execute() {
	rootCmd := &cobra.Command{
		Use:   "previewctl",
		Short: "Manage isolated preview and development environments",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return loadEnvFiles(globalEnvFiles)
		},
		Run: func(cmd *cobra.Command, args []string) {
			if v, _ := cmd.Flags().GetBool("version"); v {
				runVersionCheck()
				return
			}
			_ = cmd.Help()
		},
	}
	rootCmd.Flags().BoolP("version", "v", false, "Print the current version and check for updates")
	rootCmd.PersistentFlags().StringVarP(&globalMode, "mode", "m", "", "Deployment mode (local, remote). Inferred from environment state when omitted.")
	rootCmd.PersistentFlags().StringVarP(&globalEnvName, "env", "e", "", "Environment name (required for remote mode, inferred from cwd for local)")
	rootCmd.PersistentFlags().StringVar(&globalEnvFiles, "env-file", "", "Comma-separated list of env files to load (in addition to .env and .env.previewctl)")

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
		newSSHCmd(),
		newServiceCmd(),
		newStepCmd(),
		newStoreCmd(),
		newCleanCmd(),
		newReconcileCmd(),
		newMigrateCmd(),
		newVersionCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		version.CheckForUpdate()
		os.Exit(1)
	}
}

// resolveMode determines the deployment mode. Priority:
//  1. Explicit --mode flag (non-empty)
//  2. Inferred from stored environment state (when --env is set)
//  3. Default to "local"
func resolveMode() (string, error) {
	if globalMode != "" {
		return globalMode, nil
	}

	// Try to infer from stored environment
	if globalEnvName != "" {
		if mode, err := inferModeFromState(globalEnvName); err == nil && mode != "" {
			return mode, nil
		}
	}

	return "local", nil
}

// inferModeFromState looks up an environment in available state sources
// and returns its stored mode. Checks Postgres first (if DSN available),
// then falls back to file state.
func inferModeFromState(envName string) (string, error) {
	// Load base config (no overlay) to get the project name
	baseCfg, _, err := loadConfigWithMode("local")
	if err != nil {
		return "", err
	}

	// Try Postgres state if DSN is available
	dsn := os.Getenv("PREVIEWCTL_STATE_DSN")
	if dsn != "" {
		pgAdapter, err := filestate.NewPostgresStateAdapter(dsn, baseCfg.Name)
		if err == nil {
			entry, err := pgAdapter.GetEnvironment(context.Background(), envName)
			if err == nil && entry != nil {
				return string(entry.Mode), nil
			}
		}
	}

	// Try file state
	home, _ := os.UserHomeDir()
	statePath := filepath.Join(home, ".cache", "previewctl", baseCfg.Name, "state.json")
	fileAdapter := filestate.NewFileStateAdapter(statePath)
	entry, err := fileAdapter.GetEnvironment(context.Background(), envName)
	if err == nil && entry != nil {
		return string(entry.Mode), nil
	}

	return "", nil
}

// buildManager loads config, resolves mode, wires adapters, and creates a Manager.
func buildManager(progress domain.ProgressReporter) (*domain.Manager, *domain.ProjectConfig, error) {
	mode, err := resolveMode()
	if err != nil {
		return nil, nil, err
	}
	return buildManagerWithMode(progress, mode)
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

// resolvedMode returns the mode that buildManager would use.
// Convenience for CLI commands that need to check the mode.
func resolvedMode() string {
	mode, err := resolveMode()
	if err != nil {
		return "local"
	}
	return mode
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

// requireEnv resolves the environment name from the --env flag or cwd.
func requireEnv(statePath string) (string, error) {
	if globalEnvName != "" {
		return globalEnvName, nil
	}

	// Try to resolve from cwd (local mode)
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting cwd: %w", err)
	}

	state := filestate.NewFileStateAdapter(statePath)
	fullState, err := state.Load(context.TODO())
	if err != nil {
		return "", fmt.Errorf("loading state: %w", err)
	}

	envName, err := domain.ResolveEnvironmentFromCwd(cwd, fullState.Environments)
	if err != nil {
		return "", fmt.Errorf("--env (-e) is required (could not infer from cwd: %w)", err)
	}
	return envName, nil
}

// loadEnvFiles loads environment variables from default files (.env, .env.previewctl)
// and any additional files specified via --env-file. Files are loaded as overlays —
// later files override earlier ones. Variables already set in the environment
// (e.g., THIS_VAR=x previewctl ...) take priority over all files.
func loadEnvFiles(extraFiles string) error {
	// Snapshot existing env vars so we can preserve manual overrides
	existing := make(map[string]bool)
	for _, e := range os.Environ() {
		if k, _, ok := strings.Cut(e, "="); ok {
			existing[k] = true
		}
	}

	// Default files (silently skip if missing)
	defaults := []string{".env", ".env.previewctl"}
	for _, f := range defaults {
		if err := applyEnvFile(f, existing); err != nil {
			return fmt.Errorf("loading %s: %w", f, err)
		}
	}

	// Extra files from --env-file flag (error if missing)
	if extraFiles != "" {
		for _, f := range strings.Split(extraFiles, ",") {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			if _, err := os.Stat(f); err != nil {
				return fmt.Errorf("env file not found: %s", f)
			}
			if err := applyEnvFile(f, existing); err != nil {
				return fmt.Errorf("loading %s: %w", f, err)
			}
		}
	}

	return nil
}

// applyEnvFile reads a KEY=VALUE file and sets env vars.
// Variables in the `preserve` set are not overwritten (they were set before any file loading).
// Missing files are silently skipped.
func applyEnvFile(path string, preserve map[string]bool) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // default files may not exist
		}
		return err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// Strip surrounding quotes from value
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		// Don't override manually set variables
		if preserve[key] {
			continue
		}
		_ = os.Setenv(key, value)
	}
	return scanner.Err()
}
