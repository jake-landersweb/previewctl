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
// template variable references, and supported engine types.
// It does NOT check file system paths — use ValidateConfigWithFS for that.
func ValidateConfig(cfg *ProjectConfig) error {
	v := &ValidationError{}

	validateRequired(v, cfg)
	validatePortCollisions(v, cfg)
	validateDependencyRefs(v, cfg)
	validateTemplateVars(v, cfg)
	validateEngines(v, cfg)
	validateSeedConfig(v, cfg)
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
	validateEngines(v, cfg)
	validateSeedConfig(v, cfg)
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
		if svc.Port == 0 {
			v.addf("service '%s': 'port' is required", name)
		}
	}
	for name, db := range cfg.Core.Databases {
		if db.Engine == "" {
			v.addf("database '%s': 'engine' is required", name)
		}
		if db.Image == "" {
			v.addf("database '%s': 'image' is required", name)
		}
		if db.Port == 0 {
			v.addf("database '%s': 'port' is required", name)
		}
		if db.User == "" {
			v.addf("database '%s': 'user' is required", name)
		}
		if db.Password == "" {
			v.addf("database '%s': 'password' is required", name)
		}
		if db.TemplateDb == "" {
			v.addf("database '%s': 'templateDb' is required", name)
		}
	}
	for name, infra := range cfg.Infrastructure {
		if infra.Image == "" {
			v.addf("infrastructure '%s': 'image' is required", name)
		}
		if infra.Port == 0 {
			v.addf("infrastructure '%s': 'port' is required", name)
		}
	}
}

func validatePortCollisions(v *ValidationError, cfg *ProjectConfig) {
	// Check base port collisions across services and infrastructure
	portOwners := make(map[int][]string)

	for name, svc := range cfg.Services {
		portOwners[svc.Port] = append(portOwners[svc.Port], fmt.Sprintf("service '%s'", name))
	}
	for name, infra := range cfg.Infrastructure {
		portOwners[infra.Port] = append(portOwners[infra.Port], fmt.Sprintf("infrastructure '%s'", name))
	}
	for name, db := range cfg.Core.Databases {
		portOwners[db.Port] = append(portOwners[db.Port], fmt.Sprintf("database '%s'", name))
	}

	for port, owners := range portOwners {
		if len(owners) > 1 {
			v.addf("base port %d is used by multiple entries: %s", port, strings.Join(owners, ", "))
		}
	}
}

func validateDependencyRefs(v *ValidationError, cfg *ProjectConfig) {
	// Build a set of all known names (services + infrastructure)
	known := make(map[string]bool)
	for name := range cfg.Services {
		known[name] = true
	}
	for name := range cfg.Infrastructure {
		known[name] = true
	}
	for name := range cfg.Core.Databases {
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
	// Build the set of valid port references
	validPorts := make(map[string]bool)
	for name := range cfg.Services {
		validPorts[name] = true
	}
	for name := range cfg.Infrastructure {
		validPorts[name] = true
	}

	// Build the set of valid database references
	validDatabases := make(map[string]bool)
	for name := range cfg.Core.Databases {
		validDatabases[name] = true
	}

	validDbFields := map[string]bool{
		"connectionString": true,
		"host":             true,
		"port":             true,
		"user":             true,
		"password":         true,
		"database":         true,
	}

	for svcName, svc := range cfg.Services {
		for envKey, envVal := range svc.Env {
			// Find all {{...}} references
			matches := templateVarPattern.FindAllStringSubmatch(envVal, -1)
			for _, match := range matches {
				varPath := strings.TrimSpace(match[1])
				parts := strings.Split(varPath, ".")

				switch parts[0] {
				case "ports":
					if len(parts) != 2 {
						v.addf("service '%s' env '%s': invalid template var '{{%s}}' — expected {{ports.<name>}}", svcName, envKey, varPath)
					} else if !validPorts[parts[1]] {
						v.addf("service '%s' env '%s': references unknown port '{{%s}}' — '%s' is not a defined service or infrastructure", svcName, envKey, varPath, parts[1])
					}
				case "core":
					if len(parts) < 4 {
						v.addf("service '%s' env '%s': invalid template var '{{%s}}' — expected {{core.databases.<name>.<field>}}", svcName, envKey, varPath)
					} else if parts[1] != "databases" {
						v.addf("service '%s' env '%s': unknown core type '%s' in '{{%s}}'", svcName, envKey, parts[1], varPath)
					} else if !validDatabases[parts[2]] {
						v.addf("service '%s' env '%s': references unknown database '%s' in '{{%s}}'", svcName, envKey, parts[2], varPath)
					} else if !validDbFields[parts[3]] {
						v.addf("service '%s' env '%s': unknown database field '%s' in '{{%s}}'", svcName, envKey, parts[3], varPath)
					}
				default:
					v.addf("service '%s' env '%s': unknown template namespace '%s' in '{{%s}}'", svcName, envKey, parts[0], varPath)
				}
			}
		}
	}
}

func validateEngines(v *ValidationError, cfg *ProjectConfig) {
	supportedEngines := map[string]bool{
		"postgres": true,
	}

	for name, db := range cfg.Core.Databases {
		if db.Engine != "" && !supportedEngines[db.Engine] {
			v.addf("database '%s': unsupported engine '%s' (supported: %s)", name, db.Engine, "postgres")
		}
	}
}

func validateSeedConfig(v *ValidationError, cfg *ProjectConfig) {
	validStrategies := map[string]bool{
		"snapshot":   true,
		"script":     true,
		"migrations": true,
	}

	for name, db := range cfg.Core.Databases {
		if db.Seed == nil {
			continue
		}
		if db.Seed.Strategy == "" {
			v.addf("database '%s': seed 'strategy' is required when seed is configured", name)
		} else if !validStrategies[db.Seed.Strategy] {
			v.addf("database '%s': unsupported seed strategy '%s' (supported: snapshot, script, migrations)", name, db.Seed.Strategy)
		}

		if db.Seed.Strategy == "snapshot" && db.Seed.Snapshot == nil {
			v.addf("database '%s': seed strategy 'snapshot' requires 'snapshot' config", name)
		}
		if db.Seed.Strategy == "snapshot" && db.Seed.Snapshot != nil {
			if db.Seed.Snapshot.Source == "" {
				v.addf("database '%s': snapshot 'source' is required", name)
			}
			if db.Seed.Snapshot.Bucket == "" {
				v.addf("database '%s': snapshot 'bucket' is required", name)
			}
		}

		if db.Seed.Strategy == "script" && db.Seed.Script == "" {
			v.addf("database '%s': seed strategy 'script' requires 'script' path", name)
		}
	}
}

func validateLocalConfig(v *ValidationError, cfg *ProjectConfig) {
	if cfg.Local == nil {
		return
	}
	if cfg.Local.Worktree.BasePath == "" {
		v.add("local.worktree.basePath is required when local config is present")
	}
	for _, pattern := range cfg.Local.Worktree.SymlinkPatterns {
		if _, err := filepath.Match(pattern, "test"); err != nil {
			v.addf("local.worktree.symlinkPatterns: invalid glob pattern '%s': %v", pattern, err)
		}
	}
}

func validateFilePaths(v *ValidationError, cfg *ProjectConfig, projectRoot string, fileExists func(string) bool) {
	// Compose file
	if cfg.Local != nil && cfg.Local.ComposeFile != "" {
		path := filepath.Join(projectRoot, cfg.Local.ComposeFile)
		if !fileExists(path) {
			v.addf("local.composeFile: file not found: %s", path)
		}
	} else if len(cfg.Infrastructure) > 0 {
		v.add("infrastructure services defined but 'local.composeFile' is not set")
	}

	// Service paths
	for name, svc := range cfg.Services {
		path := filepath.Join(projectRoot, svc.Path)
		if !fileExists(path) {
			v.addf("service '%s': path not found: %s", name, path)
		}
	}

	// Seed script paths
	for name, db := range cfg.Core.Databases {
		if db.Seed != nil && db.Seed.Strategy == "script" && db.Seed.Script != "" {
			path := db.Seed.Script
			if !filepath.IsAbs(path) {
				path = filepath.Join(projectRoot, path)
			}
			if !fileExists(path) {
				v.addf("database '%s': seed script not found: %s", name, path)
			}
		}
	}
}
