package domain

import (
	"os"
	"testing"
)

func TestParseConfig_Airgoods(t *testing.T) {
	data, err := os.ReadFile("/Users/jake/worktrees/airgoods/feat/isolated-local-dev/previewctl.yaml")
	if err != nil {
		t.Skipf("airgoods config not found: %v", err)
	}

	cfg, err := ParseConfig(data)
	if err != nil {
		t.Fatalf("failed to parse airgoods config: %v", err)
	}

	if cfg.Name != "airgoods" {
		t.Errorf("expected name 'airgoods', got '%s'", cfg.Name)
	}
	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}
	if cfg.PackageManager != "pnpm" {
		t.Errorf("expected packageManager 'pnpm', got '%s'", cfg.PackageManager)
	}

	// Core databases
	if len(cfg.Core.Databases) != 1 {
		t.Fatalf("expected 1 database, got %d", len(cfg.Core.Databases))
	}
	db := cfg.Core.Databases["main"]
	if db.Engine != "postgres" {
		t.Errorf("expected engine 'postgres', got '%s'", db.Engine)
	}
	if db.Local == nil {
		t.Fatal("expected local config")
	}
	if db.Local.Image != "pgvector/pgvector:pg15" {
		t.Errorf("expected image 'pgvector/pgvector:pg15', got '%s'", db.Local.Image)
	}
	if db.Local.Port != 5500 {
		t.Errorf("expected port 5500, got %d", db.Local.Port)
	}
	if db.Local.TemplateDb != "dev_template" {
		t.Errorf("expected templateDb 'dev_template', got '%s'", db.Local.TemplateDb)
	}
	if len(db.Local.Seed) == 0 {
		t.Fatal("expected seed config")
	}

	// Services
	if len(cfg.Services) != 12 {
		t.Errorf("expected 12 services, got %d", len(cfg.Services))
	}

	backend := cfg.Services["backend"]
	if backend.Path != "apps/backend" {
		t.Errorf("expected backend path 'apps/backend', got '%s'", backend.Path)
	}
	if len(backend.Env) != 9 {
		t.Errorf("expected 9 backend env vars, got %d", len(backend.Env))
	}

	// Verify template vars use new format
	if backend.Env["DB_HOST_LOCAL"] != "{{core.databases.main.host}}" {
		t.Errorf("expected new template format, got '%s'", backend.Env["DB_HOST_LOCAL"])
	}
	if backend.Env["DB_NAME_LOCAL"] != "{{core.databases.main.database}}" {
		t.Errorf("expected new template format, got '%s'", backend.Env["DB_NAME_LOCAL"])
	}

	// Local config
	if cfg.Local == nil {
		t.Fatal("expected local config")
	}

	// Test port allocation produces valid results
	serviceNames := cfg.ServiceNames()
	if len(serviceNames) < 12 {
		t.Errorf("expected at least 12 service names, got %d", len(serviceNames))
	}

	// Test template rendering with this config
	ports, err := AllocatePortBlock("feat-auth", serviceNames)
	if err != nil {
		t.Fatalf("failed to allocate ports: %v", err)
	}

	dbInfo := &DatabaseInfo{
		Host:             "localhost",
		Port:             5500,
		User:             "postgres",
		Password:         "Paghf123-1",
		Database:         "wt_feat_auth",
		ConnectionString: "postgresql://postgres:Paghf123-1@localhost:5500/wt_feat_auth",
	}

	// Split ports into service and infra
	servicePorts := make(PortMap)
	infraPorts := make(PortMap)
	for name, port := range ports {
		if _, ok := cfg.Services[name]; ok {
			servicePorts[name] = port
		} else {
			infraPorts[name] = port
		}
	}

	ctx := &TemplateContext{
		ServicePorts: servicePorts,
		InfraPorts:   infraPorts,
		Databases:    map[string]*DatabaseInfo{"main": dbInfo},
	}

	rendered, err := RenderEnvMap(backend.Env, ctx)
	if err != nil {
		t.Fatalf("failed to render backend env: %v", err)
	}

	// Verify the port is in the allocated range
	renderedPort := rendered["PORT"]
	if renderedPort == "" || renderedPort == "0" {
		t.Errorf("expected PORT to be allocated, got '%s'", renderedPort)
	}
	if rendered["DB_HOST_LOCAL"] != "localhost" {
		t.Errorf("expected DB_HOST_LOCAL 'localhost', got '%s'", rendered["DB_HOST_LOCAL"])
	}
	if rendered["DB_PORT_LOCAL"] != "5500" {
		t.Errorf("expected DB_PORT_LOCAL '5500', got '%s'", rendered["DB_PORT_LOCAL"])
	}
	if rendered["DB_NAME_LOCAL"] != "wt_feat_auth" {
		t.Errorf("expected DB_NAME_LOCAL 'wt_feat_auth', got '%s'", rendered["DB_NAME_LOCAL"])
	}
	if rendered["DB_USER_NAME_LOCAL"] != "postgres" {
		t.Errorf("expected DB_USER_NAME_LOCAL 'postgres', got '%s'", rendered["DB_USER_NAME_LOCAL"])
	}
}

