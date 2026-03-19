package domain

import (
	"strings"
	"testing"
)

func TestValidateConfig_Valid(t *testing.T) {
	cfg := &ProjectConfig{
		Version: 1,
		Name:    "myproject",
		Core: CoreConfig{
			Databases: map[string]DatabaseConfig{
				"main": {Engine: "postgres", Local: &DatabaseModeConfig{Image: "postgres:16", Port: 5500, User: "postgres", Password: "pass", TemplateDb: "dev_template"}},
			},
		},
		InfraServices: map[string]InfraService{
			"redis": {Name: "redis", Image: "redis:7-alpine", Port: 6379},
		},
		Services: map[string]ServiceConfig{
			"backend": {Path: "apps/backend", DependsOn: []string{"redis"}, Env: map[string]string{
				"PORT":         "{{services.backend.port}}",
				"DATABASE_URL": "{{core.databases.main.connection_string}}",
				"REDIS":        "redis://localhost:{{infrastructure.redis.port}}",
			}},
			"web": {Path: "apps/web", DependsOn: []string{"backend"}, Env: map[string]string{
				"PORT": "{{services.web.port}}",
			}},
		},
	}

	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestValidateConfig_PortCollisions(t *testing.T) {
	cfg := &ProjectConfig{
		Version: 1,
		Name:    "test",
		Core: CoreConfig{
			Databases: map[string]DatabaseConfig{
				"db1": {Engine: "postgres", Local: &DatabaseModeConfig{Image: "pg:16", Port: 5500, User: "u", Password: "p", TemplateDb: "t"}},
				"db2": {Engine: "postgres", Local: &DatabaseModeConfig{Image: "pg:16", Port: 5500, User: "u", Password: "p", TemplateDb: "t2"}},
			},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected port collision error")
	}
	if !strings.Contains(err.Error(), "base port 5500") {
		t.Errorf("expected port collision message, got: %v", err)
	}
}

func TestValidateConfig_PortCollisionAcrossTypes(t *testing.T) {
	cfg := &ProjectConfig{
		Version: 1,
		Name:    "test",
		InfraServices: map[string]InfraService{
			"redis": {Name: "redis", Image: "redis", Port: 5500},
		},
		Core: CoreConfig{
			Databases: map[string]DatabaseConfig{
				"main": {Engine: "postgres", Local: &DatabaseModeConfig{Image: "pg:16", Port: 5500, User: "u", Password: "p", TemplateDb: "t"}},
			},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected port collision error")
	}
	if !strings.Contains(err.Error(), "base port 5500") {
		t.Errorf("expected cross-type collision, got: %v", err)
	}
}

func TestValidateConfig_UnknownDependency(t *testing.T) {
	cfg := &ProjectConfig{
		Version: 1,
		Name:    "test",
		Services: map[string]ServiceConfig{
			"web": {Path: "web", DependsOn: []string{"nonexistent"}},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected unknown dependency error")
	}
	if !strings.Contains(err.Error(), "unknown service 'nonexistent'") {
		t.Errorf("expected unknown dep message, got: %v", err)
	}
}

func TestValidateConfig_SelfDependency(t *testing.T) {
	cfg := &ProjectConfig{
		Version: 1,
		Name:    "test",
		Services: map[string]ServiceConfig{
			"web": {Path: "web", DependsOn: []string{"web"}},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected self-dependency error")
	}
	if !strings.Contains(err.Error(), "cannot depend on itself") {
		t.Errorf("expected self-dep message, got: %v", err)
	}
}

func TestValidateConfig_DependencyCycle(t *testing.T) {
	cfg := &ProjectConfig{
		Version: 1,
		Name:    "test",
		Services: map[string]ServiceConfig{
			"a": {Path: "a", DependsOn: []string{"b"}},
			"b": {Path: "b", DependsOn: []string{"a"}},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected cycle message, got: %v", err)
	}
}

func TestValidateConfig_InvalidTemplateVar(t *testing.T) {
	tests := []struct {
		name    string
		envVal  string
		errPart string
	}{
		{"unknown namespace", "{{foo.bar}}", "unknown template namespace"},
		{"unknown service", "{{services.nonexistent.port}}", "unknown service"},
		{"unknown infra", "{{infrastructure.nonexistent.port}}", "unknown infrastructure"},
		{"unknown database", "{{core.databases.nope.host}}", "unknown database"},
		{"unknown db field", "{{core.databases.main.nope}}", "unknown database field"},
		{"bad core type", "{{core.caches.main.host}}", "unknown core type"},
		{"malformed services", "{{services.backend}}", "expected {{services.<name>.port}}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ProjectConfig{
				Version: 1,
				Name:    "test",
				Core: CoreConfig{
					Databases: map[string]DatabaseConfig{
						"main": {Engine: "postgres", Local: &DatabaseModeConfig{Image: "pg:16", Port: 5500, User: "u", Password: "p", TemplateDb: "t"}},
					},
				},
				Services: map[string]ServiceConfig{
					"svc": {Path: "svc", Env: map[string]string{"VAR": tt.envVal}},
				},
			}

			err := ValidateConfig(cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.errPart) {
				t.Errorf("expected '%s' in error, got: %v", tt.errPart, err)
			}
		})
	}
}

func TestValidateConfig_UnsupportedEngine(t *testing.T) {
	cfg := &ProjectConfig{
		Version: 1,
		Name:    "test",
		Core: CoreConfig{
			Databases: map[string]DatabaseConfig{
				"main": {Engine: "mysql", Local: &DatabaseModeConfig{Image: "mysql:8", Port: 3306, User: "root", Password: "pass", TemplateDb: "tmpl"}},
			},
		},
		Services: map[string]ServiceConfig{
			"svc": {Path: "svc"},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected unsupported engine error")
	}
	if !strings.Contains(err.Error(), "unsupported engine 'mysql'") {
		t.Errorf("expected engine message, got: %v", err)
	}
}

func TestValidateConfig_SeedPipelineMultipleFields(t *testing.T) {
	cfg := &ProjectConfig{
		Version: 1,
		Name:    "test",
		Core: CoreConfig{
			Databases: map[string]DatabaseConfig{
				"main": {Engine: "postgres", Local: &DatabaseModeConfig{Image: "pg:16", Port: 5500, User: "u", Password: "p", TemplateDb: "t",
					Seed: []SeedStep{{SQL: "schema.sql", Dump: "data.dump"}},
				}},
			},
		},
		Services: map[string]ServiceConfig{
			"svc": {Path: "svc"},
		},
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for seed step with multiple fields")
	}
	if !strings.Contains(err.Error(), "exactly one field set") {
		t.Errorf("unexpected error: %v", err)
	}
}



func TestValidateConfig_MultipleErrors(t *testing.T) {
	cfg := &ProjectConfig{} // empty — should have many errors

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected errors")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if len(ve.Errors) < 2 {
		t.Errorf("expected multiple errors, got %d", len(ve.Errors))
	}
}

func TestValidateConfigWithFS_MissingPaths(t *testing.T) {
	cfg := &ProjectConfig{
		Version: 1,
		Name:    "test",
		InfraServices: map[string]InfraService{
			"redis": {Name: "redis", Image: "redis", Port: 6379},
		},
		Infrastructure: &InfrastructureConfig{ComposeFile: "compose.worktree.yaml"},
		Services: map[string]ServiceConfig{
			"backend": {Path: "apps/backend"},
		},
		Local: &LocalConfig{
			Worktree: WorktreeConfig{},
		},
	}

	// Nothing exists
	fileExists := func(path string) bool { return false }

	err := ValidateConfigWithFS(cfg, "/project", fileExists)
	if err == nil {
		t.Fatal("expected errors")
	}
	ve := err.(*ValidationError)

	hasCompose := false
	hasServicePath := false
	for _, e := range ve.Errors {
		if strings.Contains(e, "compose_file") {
			hasCompose = true
		}
		if strings.Contains(e, "path not found") {
			hasServicePath = true
		}
	}
	if !hasCompose {
		t.Error("expected compose file error")
	}
	if !hasServicePath {
		t.Error("expected service path error")
	}
}

func TestValidateConfigWithFS_AllExists(t *testing.T) {
	cfg := &ProjectConfig{
		Version: 1,
		Name:    "test",
		InfraServices: map[string]InfraService{
			"redis": {Name: "redis", Image: "redis", Port: 6379},
		},
		Infrastructure: &InfrastructureConfig{ComposeFile: "compose.worktree.yaml"},
		Services: map[string]ServiceConfig{
			"backend": {Path: "apps/backend"},
		},
		Local: &LocalConfig{
			Worktree: WorktreeConfig{},
		},
	}

	fileExists := func(path string) bool { return true }

	err := ValidateConfigWithFS(cfg, "/project", fileExists)
	if err != nil {
		t.Fatalf("expected no errors, got: %v", err)
	}
}
