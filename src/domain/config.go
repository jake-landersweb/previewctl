package domain

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ProjectConfig is the top-level previewctl.yaml configuration.
type ProjectConfig struct {
	Version        int                        `yaml:"version"`
	Name           string                     `yaml:"name"`
	PackageManager string                     `yaml:"package_manager,omitempty"`
	Core           CoreConfig                 `yaml:"core"`
	Infrastructure *InfrastructureConfig      `yaml:"infrastructure,omitempty"`
	Services       map[string]ServiceConfig   `yaml:"services"`
	Local          *LocalConfig               `yaml:"local,omitempty"`
	Hooks          HooksConfig                `yaml:"hooks,omitempty"`

	// InfraServices is populated by parsing the compose file referenced in
	// Infrastructure.ComposeFile. It is not read from YAML directly.
	InfraServices map[string]InfraService `yaml:"-"`
}

// CoreConfig holds managed services requiring engine-specific lifecycle.
type CoreConfig struct {
	Databases map[string]DatabaseConfig `yaml:"databases,omitempty"`
}

// DatabaseConfig defines a core database. Engine is constant;
// provider and seed config vary by deployment mode.
type DatabaseConfig struct {
	Engine string              `yaml:"engine"`
	Local  *DatabaseModeConfig `yaml:"local,omitempty"`
	// Remote *DatabaseModeConfig `yaml:"remote,omitempty"` // future
}

// DatabaseModeConfig holds provider-specific config for a deployment mode.
type DatabaseModeConfig struct {
	Provider   string     `yaml:"provider"`              // "docker", "neon", "remote"
	Image      string     `yaml:"image,omitempty"`       // docker
	Port       int        `yaml:"port,omitempty"`        // docker, remote
	User       string     `yaml:"user,omitempty"`        // docker, remote
	Password   string     `yaml:"password,omitempty"`    // docker, remote
	TemplateDb string     `yaml:"template_db,omitempty"`  // docker, remote
	Seed       []SeedStep `yaml:"seed,omitempty"`        // ordered pipeline
}

// SeedStep is a single step in the seed pipeline.
// Exactly one field should be set.
type SeedStep struct {
	SQL  string `yaml:"sql,omitempty"`
	Dump string `yaml:"dump,omitempty"`
	Run  string `yaml:"run,omitempty"`
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
	for name, db := range cfg.Core.Databases {
		if db.Engine == "" {
			return fmt.Errorf("config validation: database '%s' requires 'engine'", name)
		}
		if db.Local == nil {
			return fmt.Errorf("config validation: database '%s' requires 'local' config", name)
		}
		if db.Local.Image == "" {
			return fmt.Errorf("config validation: database '%s' requires 'local.image'", name)
		}
		if db.Local.Port == 0 {
			return fmt.Errorf("config validation: database '%s' requires 'local.port'", name)
		}
	}
	return nil
}

