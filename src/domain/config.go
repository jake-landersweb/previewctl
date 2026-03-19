package domain

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ProjectConfig is the top-level previewctl.yaml configuration.
type ProjectConfig struct {
	Version        int                      `yaml:"version"`
	Name           string                   `yaml:"name"`
	Core           CoreConfig               `yaml:"core"`
	Infrastructure *InfrastructureConfig    `yaml:"infrastructure,omitempty"`
	Services       map[string]ServiceConfig `yaml:"services"`
	Local          *LocalConfig             `yaml:"local,omitempty"`
	Hooks          HooksConfig              `yaml:"hooks,omitempty"`

	// Mode is the deployment mode (e.g., "local"). Set at load time, not from YAML.
	Mode string `yaml:"-"`

	// InfraServices is populated by parsing the compose file referenced in
	// Infrastructure.ComposeFile. It is not read from YAML directly.
	InfraServices map[string]InfraService `yaml:"-"`
}

// CoreConfig holds managed core services with hook-driven lifecycle.
type CoreConfig struct {
	Services map[string]CoreServiceConfig `yaml:"services,omitempty"`
}

// CoreServiceConfig defines a core service managed by hooks.
type CoreServiceConfig struct {
	Outputs []string          `yaml:"outputs,omitempty"`
	Hooks   *CoreServiceHooks `yaml:"hooks,omitempty"`
}

// CoreServiceHooks defines lifecycle hooks for a core service.
type CoreServiceHooks struct {
	Init    string `yaml:"init,omitempty"`
	Seed    string `yaml:"seed,omitempty"`
	Reset   string `yaml:"reset,omitempty"`
	Destroy string `yaml:"destroy,omitempty"`
}

// InfrastructureConfig holds infrastructure configuration.
type InfrastructureConfig struct {
	ComposeFile string `yaml:"compose_file"`
}

// ServiceConfig defines an application service.
type ServiceConfig struct {
	Path      string            `yaml:"path"`
	Command   string            `yaml:"command,omitempty"`
	DependsOn []string          `yaml:"depends_on,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	EnvFile   string            `yaml:"env_file,omitempty"` // relative to path, defaults to ".env.local"
}

// ResolvedEnvFile returns the env file path relative to the service path.
// Defaults to ".env.local" if not configured.
func (s ServiceConfig) ResolvedEnvFile() string {
	if s.EnvFile != "" {
		return s.EnvFile
	}
	return ".env.local"
}

// LocalConfig holds local-mode specific configuration.
type LocalConfig struct {
	Worktree WorktreeConfig `yaml:"worktree"`
}

// WorktreeConfig defines worktree settings.
type WorktreeConfig struct {
	// SymlinkPatterns are glob patterns for gitignored files to symlink from the
	// main worktree into each new worktree (e.g. ".env" matches .env files recursively).
	// These are typically secret/config files that exist in the main repo but aren't
	// tracked by git. Each pattern is matched recursively across the entire repo.
	SymlinkPatterns []string `yaml:"symlink_patterns,omitempty"`
}

// WorktreeBasePath returns the fixed base path for all previewctl worktrees.
// Worktrees are always stored in ~/.previewctl/worktrees to avoid conflicts
// with user-managed worktrees.
func WorktreeBasePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".previewctl", "worktrees")
}

// LoadConfig reads and parses a previewctl.yaml file.
func LoadConfig(path string) (*ProjectConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	return ParseConfig(data)
}

// ParseConfig parses YAML bytes into a ProjectConfig.
func ParseConfig(data []byte) (*ProjectConfig, error) {
	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadConfigWithOverlay loads a base config and merges a mode-specific overlay if present.
func LoadConfigWithOverlay(basePath, mode string) (*ProjectConfig, error) {
	cfg, err := LoadConfig(basePath)
	if err != nil {
		return nil, err
	}

	cfg.Mode = mode

	// Check for overlay file
	dir := filepath.Dir(basePath)
	ext := filepath.Ext(basePath)
	base := basePath[:len(basePath)-len(ext)]
	_ = base
	overlayPath := filepath.Join(dir, fmt.Sprintf("previewctl.%s.yaml", mode))

	overlayData, err := os.ReadFile(overlayPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading overlay config: %w", err)
	}

	var overlay ProjectConfig
	if err := yaml.Unmarshal(overlayData, &overlay); err != nil {
		return nil, fmt.Errorf("parsing overlay config: %w", err)
	}

	deepMergeConfig(cfg, &overlay)
	return cfg, nil
}

// deepMergeConfig merges overlay into base. Maps merge by key, scalars in overlay override base.
func deepMergeConfig(base, overlay *ProjectConfig) {
	if overlay.Version != 0 {
		base.Version = overlay.Version
	}
	if overlay.Name != "" {
		base.Name = overlay.Name
	}

	// Merge Core
	if overlay.Core.Services != nil {
		if base.Core.Services == nil {
			base.Core.Services = make(map[string]CoreServiceConfig)
		}
		for k, overlaySvc := range overlay.Core.Services {
			baseSvc, exists := base.Core.Services[k]
			if !exists {
				base.Core.Services[k] = overlaySvc
				continue
			}
			// Merge: overlay outputs replace base outputs if present
			if len(overlaySvc.Outputs) > 0 {
				baseSvc.Outputs = overlaySvc.Outputs
			}
			// Merge: overlay hooks replace base hooks if present
			if overlaySvc.Hooks != nil {
				baseSvc.Hooks = overlaySvc.Hooks
			}
			base.Core.Services[k] = baseSvc
		}
	}

	if overlay.Infrastructure != nil {
		base.Infrastructure = overlay.Infrastructure
	}

	// Merge Services
	if overlay.Services != nil {
		if base.Services == nil {
			base.Services = make(map[string]ServiceConfig)
		}
		for k, v := range overlay.Services {
			base.Services[k] = v
		}
	}

	if overlay.Local != nil {
		base.Local = overlay.Local
	}
	if overlay.Hooks != nil {
		base.Hooks = overlay.Hooks
	}
}

func validateConfig(cfg *ProjectConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("config validation: 'name' is required")
	}
	if cfg.Version == 0 {
		return fmt.Errorf("config validation: 'version' is required")
	}
	for name, svc := range cfg.Services {
		if svc.Path == "" {
			return fmt.Errorf("config validation: service '%s' requires 'path'", name)
		}
	}
	for name, svc := range cfg.Core.Services {
		if len(svc.Outputs) == 0 {
			return fmt.Errorf("config validation: core service '%s' requires 'outputs'", name)
		}
	}
	return nil
}
