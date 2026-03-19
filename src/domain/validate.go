package domain

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidationError collects multiple validation issues.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%d validation error(s):\n  - %s", len(e.Errors), strings.Join(e.Errors, "\n  - "))
}

func (e *ValidationError) add(msg string) {
	e.Errors = append(e.Errors, msg)
}

func (e *ValidationError) addf(format string, args ...interface{}) {
	e.Errors = append(e.Errors, fmt.Sprintf(format, args...))
}

func (e *ValidationError) hasErrors() bool {
	return len(e.Errors) > 0
}

// ValidateConfig performs deep validation of a ProjectConfig.
// It checks structural correctness, port collisions, dependency references,
// template variable references, and local config.
// It does NOT check file system paths — use ValidateConfigWithFS for that.
func ValidateConfig(cfg *ProjectConfig) error {
	v := &ValidationError{}

	validateRequired(v, cfg)
	validatePortCollisions(v, cfg)
	validateDependencyRefs(v, cfg)
	validateTemplateVars(v, cfg)
	validateLocalConfig(v, cfg)

	if v.hasErrors() {
		return v
	}
	return nil
}

// ValidateConfigWithFS performs all config validation plus file system checks.
// projectRoot is the directory containing previewctl.yaml.
// fileExists is a function that checks if a path exists (allows testing).
func ValidateConfigWithFS(cfg *ProjectConfig, projectRoot string, fileExists func(string) bool) error {
	v := &ValidationError{}

	validateRequired(v, cfg)
	validatePortCollisions(v, cfg)
	validateDependencyRefs(v, cfg)
	validateTemplateVars(v, cfg)
	validateLocalConfig(v, cfg)
	validateFilePaths(v, cfg, projectRoot, fileExists)

	if v.hasErrors() {
		return v
	}
	return nil
}

func validateRequired(v *ValidationError, cfg *ProjectConfig) {
	if cfg.Name == "" {
		v.add("'name' is required")
	}
	if cfg.Version == 0 {
		v.add("'version' is required")
	}
	if cfg.Version != 0 && cfg.Version != 1 {
		v.addf("unsupported config version %d (supported: 1)", cfg.Version)
	}

	for name, svc := range cfg.Services {
		if svc.Path == "" {
			v.addf("service '%s': 'path' is required", name)
		}
		if svc.EnvFile != "" && filepath.IsAbs(svc.EnvFile) {
			v.addf("service '%s': env_file must be a relative path, got '%s'", name, svc.EnvFile)
		}
	}
	for name, svc := range cfg.Core.Services {
		if len(svc.Outputs) == 0 {
			v.addf("core service '%s': 'outputs' is required", name)
		}
	}
	if cfg.Infrastructure != nil && cfg.Infrastructure.ComposeFile == "" {
		v.add("infrastructure.compose_file is required when infrastructure is configured")
	}
}

func validatePortCollisions(v *ValidationError, cfg *ProjectConfig) {
	// Check base port collisions across infrastructure
	portOwners := make(map[int][]string)

	for name, infra := range cfg.InfraServices {
		portOwners[infra.Port] = append(portOwners[infra.Port], fmt.Sprintf("infrastructure '%s'", name))
	}

	for port, owners := range portOwners {
		if len(owners) > 1 {
			v.addf("base port %d is used by multiple entries: %s", port, strings.Join(owners, ", "))
		}
	}
}

func validateDependencyRefs(v *ValidationError, cfg *ProjectConfig) {
	// Build a set of all known names (services + infrastructure + core services)
	known := make(map[string]bool)
	for name := range cfg.Services {
		known[name] = true
	}
	for name := range cfg.InfraServices {
		known[name] = true
	}
	for name := range cfg.Core.Services {
		known[name] = true
	}

	for svcName, svc := range cfg.Services {
		for _, dep := range svc.DependsOn {
			if !known[dep] {
				v.addf("service '%s': depends on unknown service '%s'", svcName, dep)
			}
			if dep == svcName {
				v.addf("service '%s': cannot depend on itself", svcName)
			}
		}
	}

	// Check for cycles
	if len(cfg.Services) > 0 {
		if _, err := TopologicalSort(cfg.Services); err != nil {
			v.addf("dependency cycle detected: %v", err)
		}
	}
}

func validateTemplateVars(v *ValidationError, cfg *ProjectConfig) {
	validServices := make(map[string]bool)
	for name := range cfg.Services {
		validServices[name] = true
	}

	validInfra := make(map[string]bool)
	for name := range cfg.InfraServices {
		validInfra[name] = true
	}

	for svcName, svc := range cfg.Services {
		for envKey, envVal := range svc.Env {
			matches := templateVarPattern.FindAllStringSubmatch(envVal, -1)
			for _, match := range matches {
				varPath := strings.TrimSpace(match[1])
				parts := strings.Split(varPath, ".")

				switch parts[0] {
				case "self":
					if len(parts) != 2 || parts[1] != "port" {
						v.addf("service '%s' env '%s': invalid template var '{{%s}}' — expected {{self.port}}", svcName, envKey, varPath)
					}
				case "services":
					if len(parts) != 3 || parts[2] != "port" {
						v.addf("service '%s' env '%s': invalid template var '{{%s}}' — expected {{services.<name>.port}}", svcName, envKey, varPath)
					} else if !validServices[parts[1]] {
						v.addf("service '%s' env '%s': references unknown service '%s' in '{{%s}}'", svcName, envKey, parts[1], varPath)
					}
				case "infrastructure":
					if len(parts) != 3 || parts[2] != "port" {
						v.addf("service '%s' env '%s': invalid template var '{{%s}}' — expected {{infrastructure.<name>.port}}", svcName, envKey, varPath)
					} else if !validInfra[parts[1]] {
						v.addf("service '%s' env '%s': references unknown infrastructure '%s' in '{{%s}}'", svcName, envKey, parts[1], varPath)
					}
				case "core":
					if len(parts) != 3 {
						v.addf("service '%s' env '%s': invalid template var '{{%s}}' — expected {{core.<service>.<OUTPUT>}}", svcName, envKey, varPath)
					} else {
						coreSvc, ok := cfg.Core.Services[parts[1]]
						if !ok {
							v.addf("service '%s' env '%s': references unknown core service '%s' in '{{%s}}'", svcName, envKey, parts[1], varPath)
						} else {
							found := false
							for _, o := range coreSvc.Outputs {
								if o == parts[2] {
									found = true
									break
								}
							}
							if !found {
								v.addf("service '%s' env '%s': unknown output '%s' for core service '%s' in '{{%s}}'", svcName, envKey, parts[2], parts[1], varPath)
							}
						}
					}
				default:
					v.addf("service '%s' env '%s': unknown template namespace '%s' in '{{%s}}'", svcName, envKey, parts[0], varPath)
				}
			}
		}
	}
}

func validateLocalConfig(v *ValidationError, cfg *ProjectConfig) {
	if cfg.Local == nil {
		return
	}
	for _, pattern := range cfg.Local.Worktree.SymlinkPatterns {
		if _, err := filepath.Match(pattern, "test"); err != nil {
			v.addf("local.worktree.symlink_patterns: invalid glob pattern '%s': %v", pattern, err)
		}
	}
}

func validateFilePaths(v *ValidationError, cfg *ProjectConfig, projectRoot string, fileExists func(string) bool) {
	// Infrastructure compose file
	if cfg.Infrastructure != nil && cfg.Infrastructure.ComposeFile != "" {
		path := filepath.Join(projectRoot, cfg.Infrastructure.ComposeFile)
		if !fileExists(path) {
			v.addf("infrastructure.compose_file: file not found: %s", path)
		}
	}

	// Service paths and env_file path escape checks
	for name, svc := range cfg.Services {
		path := filepath.Join(projectRoot, svc.Path)
		if !fileExists(path) {
			v.addf("service '%s': path not found: %s", name, path)
		}
		if svc.EnvFile != "" {
			envFilePath := filepath.Join(projectRoot, svc.Path, svc.EnvFile)
			resolved, err := filepath.Abs(envFilePath)
			if err == nil {
				absRoot, _ := filepath.Abs(projectRoot)
				if !strings.HasPrefix(resolved, absRoot+string(filepath.Separator)) && resolved != absRoot {
					v.addf("service '%s': env_file '%s' resolves outside the project root", name, svc.EnvFile)
				}
			}
		}
	}

	// Core service hook file path validation
	for name, svc := range cfg.Core.Services {
		if svc.Hooks == nil {
			continue
		}
		for action, script := range map[string]string{"init": svc.Hooks.Init, "seed": svc.Hooks.Seed, "reset": svc.Hooks.Reset, "destroy": svc.Hooks.Destroy} {
			if script == "" {
				continue
			}
			cmd := strings.Fields(script)[0]
			if strings.HasPrefix(cmd, "./") || strings.HasPrefix(cmd, "/") {
				path := cmd
				if !filepath.IsAbs(path) {
					path = filepath.Join(projectRoot, path)
				}
				if !fileExists(path) {
					v.addf("core service '%s': hook '%s' script not found: %s", name, action, path)
				}
			}
		}
	}
}
