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
		if db.Local == nil {
			v.addf("database '%s': 'local' config is required", name)
			continue
		}
		if db.Local.Image == "" {
			v.addf("database '%s': 'local.image' is required", name)
		}
		if db.Local.Port == 0 {
			v.addf("database '%s': 'local.port' is required", name)
		}
		if db.Local.User == "" {
			v.addf("database '%s': 'local.user' is required", name)
		}
		if db.Local.Password == "" {
			v.addf("database '%s': 'local.password' is required", name)
		}
		if db.Local.TemplateDb == "" {
			v.addf("database '%s': 'local.template_db' is required", name)
		}
	}
	if cfg.Infrastructure != nil && cfg.Infrastructure.ComposeFile == "" {
		v.add("infrastructure.compose_file is required when infrastructure is configured")
	}
}

func validatePortCollisions(v *ValidationError, cfg *ProjectConfig) {
	// Check base port collisions across services and infrastructure
	portOwners := make(map[int][]string)

	for name, svc := range cfg.Services {
		portOwners[svc.Port] = append(portOwners[svc.Port], fmt.Sprintf("service '%s'", name))
	}
	for name, infra := range cfg.InfraServices {
		portOwners[infra.Port] = append(portOwners[infra.Port], fmt.Sprintf("infrastructure '%s'", name))
	}
	for name, db := range cfg.Core.Databases {
		if db.Local != nil {
			portOwners[db.Local.Port] = append(portOwners[db.Local.Port], fmt.Sprintf("database '%s'", name))
		}
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
	for name := range cfg.InfraServices {
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
	for name := range cfg.InfraServices {
		validPorts[name] = true
	}

	// Build the set of valid database references
	validDatabases := make(map[string]bool)
	for name := range cfg.Core.Databases {
		validDatabases[name] = true
	}

	validDbFields := map[string]bool{
		"connection_string": true,
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
	for name, db := range cfg.Core.Databases {
		if db.Local == nil || len(db.Local.Seed) == 0 {
			continue
		}
		for i, step := range db.Local.Seed {
			fieldsSet := 0
			if step.SQL != "" {
				fieldsSet++
			}
			if step.Dump != "" {
				fieldsSet++
			}
			if step.S3 != nil {
				fieldsSet++
			}
			if step.Run != "" {
				fieldsSet++
			}
			if fieldsSet == 0 {
				v.addf("database '%s': seed[%d] must have exactly one field set (sql, dump, s3, or run)", name, i)
			}
			if fieldsSet > 1 {
				v.addf("database '%s': seed[%d] must have exactly one field set, got %d", name, i, fieldsSet)
			}
			if step.S3 != nil {
				if step.S3.Bucket == "" {
					v.addf("database '%s': seed[%d].s3 'bucket' is required", name, i)
				}
				if step.S3.Key == "" {
					v.addf("database '%s': seed[%d].s3 'key' is required", name, i)
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

	// Service paths
	for name, svc := range cfg.Services {
		path := filepath.Join(projectRoot, svc.Path)
		if !fileExists(path) {
			v.addf("service '%s': path not found: %s", name, path)
		}
	}

	// Seed file paths (sql and dump steps)
	for name, db := range cfg.Core.Databases {
		if db.Local == nil {
			continue
		}
		for i, step := range db.Local.Seed {
			if step.SQL != "" {
				path := step.SQL
				if !filepath.IsAbs(path) {
					path = filepath.Join(projectRoot, path)
				}
				if !fileExists(path) {
					v.addf("database '%s': seed[%d].sql file not found: %s", name, i, path)
				}
			}
			if step.Dump != "" {
				path := step.Dump
				if !filepath.IsAbs(path) {
					path = filepath.Join(projectRoot, path)
				}
				if !fileExists(path) {
					v.addf("database '%s': seed[%d].dump file not found: %s", name, i, path)
				}
			}
		}
	}
}
