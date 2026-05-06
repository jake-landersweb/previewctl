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
	Create  string     `yaml:"create"`
	Destroy string     `yaml:"destroy"`
	Outputs []string   `yaml:"outputs,omitempty"`
	SSH     *SSHConfig `yaml:"ssh,omitempty"`
}

// SSHConfig defines how previewctl connects to remote compute via SSH.
// All fields support {{store.KEY}} template resolution.
type SSHConfig struct {
	// ProxyCommand is the SSH ProxyCommand used to tunnel into the VM.
	// Example: "gcloud compute start-iap-tunnel {{store.VM_NAME}} %p --listen-on-stdin --zone={{store.GCP_ZONE}} --project={{store.GCP_PROJECT}}"
	ProxyCommand string `yaml:"proxy_command"`
	// User is the SSH username. Example: "{{store.SSH_USER}}"
	User string `yaml:"user"`
	// UserCommand is a shell command that resolves the SSH username dynamically.
	// Useful when different users (CI vs human) SSH into the same VM.
	// Example: "gcloud compute os-login describe-profile --format='value(posixAccounts[0].username)'"
	// Takes precedence over User when set.
	UserCommand string `yaml:"user_command"`
	// IdentityFile is the path to the SSH private key. Supports ~ expansion.
	// Example: "~/.ssh/google_compute_engine"
	IdentityFile string `yaml:"identity_file"`
	// Root is the remote working directory. Defaults to "/app".
	Root string `yaml:"root,omitempty"`
}

// RunnerConfig holds runner lifecycle hooks.
type RunnerConfig struct {
	Before CommandHook `yaml:"before,omitempty"`
	// Build, when set, runs once during build_services in place of the
	// per-service Build loop. Intended for monorepo tools (turborepo, nx,
	// lerna) where one bulk command builds everything more efficiently
	// than N filtered invocations.
	Build   CommandHook    `yaml:"build,omitempty"`
	Deploy  CommandHook    `yaml:"deploy,omitempty"`
	Destroy CommandHook    `yaml:"destroy,omitempty"`
	After   CommandHook    `yaml:"after,omitempty"`
	Compose *ComposeConfig `yaml:"compose,omitempty"`
}

// CommandHook is a command-valued hook that can be configured either as a
// legacy scalar string or as an object with hook metadata.
type CommandHook struct {
	Command    string `yaml:"command,omitempty"`
	AllowCache *bool  `yaml:"allow_cache,omitempty"`
}

// UnmarshalYAML accepts both:
//
//	runner:
//	  after: ./script.sh
//
// and:
//
//	runner:
//	  after:
//	    command: ./script.sh
//	    allow_cache: false
func (h *CommandHook) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		if value.Tag == "!!null" {
			*h = CommandHook{}
			return nil
		}
		h.Command = value.Value
		h.AllowCache = nil
		return nil
	case yaml.MappingNode:
		var raw struct {
			Command    string `yaml:"command"`
			AllowCache *bool  `yaml:"allow_cache"`
		}
		if err := value.Decode(&raw); err != nil {
			return err
		}
		h.Command = raw.Command
		h.AllowCache = raw.AllowCache
		return nil
	default:
		return fmt.Errorf("hook must be a string or object")
	}
}

// IsConfigured reports whether this hook has a command to run.
func (h CommandHook) IsConfigured() bool {
	return h.Command != ""
}

// CacheAllowed reports whether completed checkpoints may skip this hook.
// Hooks are cacheable by default for backward compatibility.
func (h CommandHook) CacheAllowed() bool {
	return h.AllowCache == nil || *h.AllowCache
}

// ComposeConfig defines how previewctl generates and manages Docker Compose
// for application services in remote mode.
type ComposeConfig struct {
	Autostart []string     `yaml:"autostart"`       // services started on create (proxy is always implicit if enabled)
	Image     string       `yaml:"image"`           // base Docker image for app containers (e.g., "node:20")
	Proxy     *ProxyConfig `yaml:"proxy,omitempty"` // reverse proxy configuration
}

// ProxyConfig defines the reverse proxy that sits in front of preview services.
type ProxyConfig struct {
	Enabled *bool  `yaml:"enabled,omitempty"` // defaults to true if omitted
	Domain  string `yaml:"domain"`            // e.g., "preview.airgoods.com"
	Type    string `yaml:"type,omitempty"`    // "nginx" (default). Future: "traefik", "caddy"
}

// IsEnabled returns whether the proxy is enabled (defaults to true).
func (p *ProxyConfig) IsEnabled() bool {
	if p == nil {
		return false
	}
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
}

// ResolvedType returns the proxy type, defaulting to "nginx".
func (p *ProxyConfig) ResolvedType() string {
	if p == nil || p.Type == "" {
		return "nginx"
	}
	return p.Type
}

// InfrastructureConfig holds infrastructure configuration.
type InfrastructureConfig struct {
	ComposeFile string `yaml:"compose_file"`
}

// ServiceConfig defines an application service.
type ServiceConfig struct {
	Path      string            `yaml:"path"`
	Port      int               `yaml:"port,omitempty"` // fixed port — skips the port allocator when set
	Command   string            `yaml:"command,omitempty"`
	DependsOn []string          `yaml:"depends_on,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	EnvFile   string            `yaml:"env_file,omitempty"` // relative to path, defaults to ".env.local"
	Build     string            `yaml:"build,omitempty"`    // build command (run on host before container starts)
	Start     string            `yaml:"start,omitempty"`    // start command (run inside container). Required for compose generation.
	Proxy     []ServiceProxy    `yaml:"proxy,omitempty"`    // optional reverse proxy rules for nginx
}

// ServiceProxy defines a reverse proxy rule on a service's subdomain.
// When configured, nginx generates a location block that proxies the given
// path to the target service's port (same-origin for IAP cookie compatibility).
type ServiceProxy struct {
	Path       string             `yaml:"path"`                  // source path the browser sends, e.g., "/api" or "/iapi"
	TargetPath string             `yaml:"target_path,omitempty"` // path rewritten to on the target service. Defaults to Path if omitted.
	To         ServiceProxyTarget `yaml:"to"`
}

// ResolvedTargetPath returns the target path, defaulting to Path if not set.
func (p *ServiceProxy) ResolvedTargetPath() string {
	if p.TargetPath != "" {
		return p.TargetPath
	}
	return p.Path
}

// ServiceProxyTarget identifies the target service for a proxy rule.
type ServiceProxyTarget struct {
	Service string `yaml:"service"` // target service name, resolved to its port at generation time
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
		if base.Provisioner.Compute == nil {
			base.Provisioner.Compute = overlay.Provisioner.Compute
		} else {
			if overlay.Provisioner.Compute.Create != "" {
				base.Provisioner.Compute.Create = overlay.Provisioner.Compute.Create
			}
			if overlay.Provisioner.Compute.Destroy != "" {
				base.Provisioner.Compute.Destroy = overlay.Provisioner.Compute.Destroy
			}
			if len(overlay.Provisioner.Compute.Outputs) > 0 {
				base.Provisioner.Compute.Outputs = overlay.Provisioner.Compute.Outputs
			}
			if overlay.Provisioner.Compute.SSH != nil {
				base.Provisioner.Compute.SSH = overlay.Provisioner.Compute.SSH
			}
		}
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
			if overlaySvc.Port != 0 {
				baseSvc.Port = overlaySvc.Port
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
			if overlaySvc.Build != "" {
				baseSvc.Build = overlaySvc.Build
			}
			if overlaySvc.Start != "" {
				baseSvc.Start = overlaySvc.Start
			}
			if len(overlaySvc.Proxy) > 0 {
				baseSvc.Proxy = overlaySvc.Proxy
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
			mergeCommandHook(&base.Runner.Before, overlay.Runner.Before)
			mergeCommandHook(&base.Runner.Build, overlay.Runner.Build)
			mergeCommandHook(&base.Runner.Deploy, overlay.Runner.Deploy)
			mergeCommandHook(&base.Runner.After, overlay.Runner.After)
			mergeCommandHook(&base.Runner.Destroy, overlay.Runner.Destroy)
			if overlay.Runner.Compose != nil {
				base.Runner.Compose = overlay.Runner.Compose
			}
		}
	}
}

func mergeCommandHook(base *CommandHook, overlay CommandHook) {
	if overlay.Command != "" {
		base.Command = overlay.Command
	}
	if overlay.AllowCache != nil {
		base.AllowCache = overlay.AllowCache
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
