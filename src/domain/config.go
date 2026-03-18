package domain

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ProjectConfig is the top-level previewctl.yaml configuration.
type ProjectConfig struct {
	Version        int                           `yaml:"version"`
	Name           string                        `yaml:"name"`
	PackageManager string                        `yaml:"packageManager,omitempty"`
	Core           CoreConfig                    `yaml:"core"`
	Infrastructure map[string]InfraServiceConfig `yaml:"infrastructure,omitempty"`
	Services       map[string]ServiceConfig      `yaml:"services"`
	Local          *LocalConfig                  `yaml:"local,omitempty"`
	Hooks          HooksConfig                   `yaml:"hooks,omitempty"`
}

// CoreConfig holds managed services requiring engine-specific lifecycle.
type CoreConfig struct {
	Databases map[string]DatabaseConfig `yaml:"databases,omitempty"`
}

// DatabaseConfig defines a core database.
type DatabaseConfig struct {
	Engine     string      `yaml:"engine"`
	Image      string      `yaml:"image"`
	Port       int         `yaml:"port"`
	User       string      `yaml:"user"`
	Password   string      `yaml:"password"`
	TemplateDb string      `yaml:"templateDb"`
	Seed       *SeedConfig `yaml:"seed,omitempty"`
}

// SeedConfig defines how a database template is seeded.
type SeedConfig struct {
	Strategy string          `yaml:"strategy"`
	Snapshot *SnapshotConfig `yaml:"snapshot,omitempty"`
	Script   string          `yaml:"script,omitempty"`
}

// SnapshotConfig defines S3 snapshot source.
type SnapshotConfig struct {
	Source string `yaml:"source"`
	Bucket string `yaml:"bucket"`
	Prefix string `yaml:"prefix"`
	Region string `yaml:"region"`
}

// InfraServiceConfig defines a generic docker infrastructure service.
type InfraServiceConfig struct {
	Image string `yaml:"image"`
	Port  int    `yaml:"port"`
}

// ServiceConfig defines an application service.
type ServiceConfig struct {
	Path      string            `yaml:"path"`
	Port      int               `yaml:"port"`
	Command   string            `yaml:"command,omitempty"`
	DependsOn []string          `yaml:"dependsOn,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
}

// LocalConfig holds local-mode specific configuration.
type LocalConfig struct {
	Worktree WorktreeConfig `yaml:"worktree"`
	// ComposeFile is the path to the docker compose file for per-env infrastructure,
	// relative to the project root. If empty, no per-env containers are started.
	ComposeFile string `yaml:"composeFile,omitempty"`
}

// WorktreeConfig defines worktree settings.
type WorktreeConfig struct {
	BasePath string `yaml:"basePath"`
	// SymlinkPatterns are glob patterns for gitignored files to symlink from the
	// main worktree into each new worktree (e.g. ".env" matches .env files recursively).
	// These are typically secret/config files that exist in the main repo but aren't
	// tracked by git. Each pattern is matched recursively across the entire repo.
	SymlinkPatterns []string `yaml:"symlinkPatterns,omitempty"`
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
		if svc.Port == 0 {
			return fmt.Errorf("config validation: service '%s' requires 'port'", name)
		}
	}
	for name, db := range cfg.Core.Databases {
		if db.Engine == "" {
			return fmt.Errorf("config validation: database '%s' requires 'engine'", name)
		}
		if db.Image == "" {
			return fmt.Errorf("config validation: database '%s' requires 'image'", name)
		}
		if db.Port == 0 {
			return fmt.Errorf("config validation: database '%s' requires 'port'", name)
		}
	}
	for name, infra := range cfg.Infrastructure {
		if infra.Image == "" {
			return fmt.Errorf("config validation: infrastructure '%s' requires 'image'", name)
		}
		if infra.Port == 0 {
			return fmt.Errorf("config validation: infrastructure '%s' requires 'port'", name)
		}
	}
	return nil
}

// AllBasePorts returns a map of all service and infrastructure names to their base ports.
func (c *ProjectConfig) AllBasePorts() map[string]int {
	ports := make(map[string]int)
	for name, svc := range c.Services {
		ports[name] = svc.Port
	}
	for name, infra := range c.Infrastructure {
		ports[name] = infra.Port
	}
	return ports
}
