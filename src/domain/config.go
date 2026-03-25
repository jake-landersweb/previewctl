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
	Provisioner    ProvisionerConfig        `yaml:"provisioner"`
	Infrastructure *InfrastructureConfig    `yaml:"infrastructure,omitempty"`
	Services       map[string]ServiceConfig `yaml:"services"`
	Runner         *RunnerConfig            `yaml:"runner,omitempty"`

	// Mode is the deployment mode (e.g., "local"). Set at load time, not from YAML.
	Mode string `yaml:"-"`

	// InfraServices is populated by parsing the compose file referenced in
	// Infrastructure.ComposeFile. It is not read from YAML directly.
	InfraServices map[string]InfraService `yaml:"-"`
}

// ProvisionerConfig holds managed provisioner services with hook-driven lifecycle.
type ProvisionerConfig struct {
	Before   string                              `yaml:"before,omitempty"`
	After    string                              `yaml:"after,omitempty"`
	Compute  *ComputeHooks                       `yaml:"compute,omitempty"`
	Services map[string]ProvisionerServiceConfig `yaml:"services,omitempty"`
}

// ProvisionerServiceConfig defines a provisioner service managed by hooks.
type ProvisionerServiceConfig struct {
	Outputs []string `yaml:"outputs,omitempty"`
	Init    string   `yaml:"init,omitempty"`
	Seed    string   `yaml:"seed,omitempty"`
	Reset   string   `yaml:"reset,omitempty"`
	Destroy string   `yaml:"destroy,omitempty"`
}

// ComputeHooks defines lifecycle hooks for compute resources.
type ComputeHooks struct {
	Create  string   `yaml:"create"`
	Destroy string   `yaml:"destroy"`
	Outputs []string `yaml:"outputs,omitempty"`
}

// RunnerConfig holds runner lifecycle hooks.
type RunnerConfig struct {
	Before  string `yaml:"before,omitempty"`
	Deploy  string `yaml:"deploy,omitempty"`
	Destroy string `yaml:"destroy,omitempty"`
	After   string `yaml:"after,omitempty"`
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

	// Merge Provisioner
	if overlay.Provisioner.Before != "" {
		base.Provisioner.Before = overlay.Provisioner.Before
	}
	if overlay.Provisioner.After != "" {
		base.Provisioner.After = overlay.Provisioner.After
	}
	if overlay.Provisioner.Compute != nil {
		base.Provisioner.Compute = overlay.Provisioner.Compute
	}
	if overlay.Provisioner.Services != nil {
		if base.Provisioner.Services == nil {
			base.Provisioner.Services = make(map[string]ProvisionerServiceConfig)
		}
		for k, overlaySvc := range overlay.Provisioner.Services {
			baseSvc, exists := base.Provisioner.Services[k]
			if !exists {
				base.Provisioner.Services[k] = overlaySvc
				continue
			}
			// Merge: overlay outputs replace base outputs if present
			if len(overlaySvc.Outputs) > 0 {
				baseSvc.Outputs = overlaySvc.Outputs
			}
			// Merge: overlay hooks override base hooks if present
			if overlaySvc.Init != "" {
				baseSvc.Init = overlaySvc.Init
			}
			if overlaySvc.Seed != "" {
				baseSvc.Seed = overlaySvc.Seed
			}
			if overlaySvc.Reset != "" {
				baseSvc.Reset = overlaySvc.Reset
			}
			if overlaySvc.Destroy != "" {
				baseSvc.Destroy = overlaySvc.Destroy
			}
			base.Provisioner.Services[k] = baseSvc
		}
	}

	if overlay.Infrastructure != nil {
		base.Infrastructure = overlay.Infrastructure
	}

	// Merge Services (field-level: overlay fields override base, env maps are additive)
	if overlay.Services != nil {
		if base.Services == nil {
			base.Services = make(map[string]ServiceConfig)
		}
		for k, overlaySvc := range overlay.Services {
			baseSvc, exists := base.Services[k]
			if !exists {
				base.Services[k] = overlaySvc
				continue
			}
			if overlaySvc.Path != "" {
				baseSvc.Path = overlaySvc.Path
			}
			if overlaySvc.Command != "" {
				baseSvc.Command = overlaySvc.Command
			}
			if len(overlaySvc.DependsOn) > 0 {
				baseSvc.DependsOn = overlaySvc.DependsOn
			}
			if overlaySvc.EnvFile != "" {
				baseSvc.EnvFile = overlaySvc.EnvFile
			}
			if overlaySvc.Env != nil {
				if baseSvc.Env == nil {
					baseSvc.Env = make(map[string]string)
				}
				for ek, ev := range overlaySvc.Env {
					baseSvc.Env[ek] = ev
				}
			}
			base.Services[k] = baseSvc
		}
	}

	// Merge Runner (field-level: overlay fields override base)
	if overlay.Runner != nil {
		if base.Runner == nil {
			base.Runner = overlay.Runner
		} else {
			if overlay.Runner.Before != "" {
				base.Runner.Before = overlay.Runner.Before
			}
			if overlay.Runner.Deploy != "" {
				base.Runner.Deploy = overlay.Runner.Deploy
			}
			if overlay.Runner.After != "" {
				base.Runner.After = overlay.Runner.After
			}
			if overlay.Runner.Destroy != "" {
				base.Runner.Destroy = overlay.Runner.Destroy
			}
		}
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
	for name, svc := range cfg.Provisioner.Services {
		if len(svc.Outputs) == 0 {
			return fmt.Errorf("config validation: provisioner service '%s' requires 'outputs'", name)
		}
	}
	return nil
}
