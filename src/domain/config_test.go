package domain

import (
	"testing"
)

func TestParseConfig_Valid(t *testing.T) {
	yaml := []byte(`
version: 1
name: myproject
package_manager: pnpm

core:
  databases:
    main:
      engine: postgres
      local:
        image: postgres:16
        port: 5500
        user: postgres
        password: postgres
        template_db: dev_template

services:
  backend:
    path: apps/backend
    port: 8000
    command: pnpm dev
    depends_on: [redis]
    env:
      PORT: "{{ports.backend}}"
  web:
    path: apps/web
    port: 3000
    depends_on: [backend]
    env:
      PORT: "{{ports.web}}"

local:
  worktree:
    symlink_patterns: [".env", ".env.development"]
`)

	cfg, err := ParseConfig(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Name != "myproject" {
		t.Errorf("expected name 'myproject', got '%s'", cfg.Name)
	}
	if cfg.PackageManager != "pnpm" {
		t.Errorf("expected packageManager 'pnpm', got '%s'", cfg.PackageManager)
	}
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
	if db.Local.Port != 5500 {
		t.Errorf("expected port 5500, got %d", db.Local.Port)
	}
	if db.Local.TemplateDb != "dev_template" {
		t.Errorf("expected templateDb 'dev_template', got '%s'", db.Local.TemplateDb)
	}

	if len(cfg.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(cfg.Services))
	}
	backend := cfg.Services["backend"]
	if backend.Command != "pnpm dev" {
		t.Errorf("expected command 'pnpm dev', got '%s'", backend.Command)
	}
	if len(backend.DependsOn) != 1 || backend.DependsOn[0] != "redis" {
		t.Errorf("expected dependsOn [redis], got %v", backend.DependsOn)
	}

	if cfg.Local == nil {
		t.Fatal("expected local config")
	}
	if len(cfg.Local.Worktree.SymlinkPatterns) != 2 {
		t.Errorf("expected 2 symlink patterns, got %d", len(cfg.Local.Worktree.SymlinkPatterns))
	}
}

func TestParseConfig_MissingName(t *testing.T) {
	yaml := []byte(`
version: 1
services:
  backend:
    path: apps/backend
    port: 8000
`)
	_, err := ParseConfig(yaml)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestParseConfig_MissingVersion(t *testing.T) {
	yaml := []byte(`
name: myproject
services:
  backend:
    path: apps/backend
    port: 8000
`)
	_, err := ParseConfig(yaml)
	if err == nil {
		t.Fatal("expected error for missing version")
	}
}

func TestParseConfig_ServiceMissingPath(t *testing.T) {
	yaml := []byte(`
version: 1
name: myproject
services:
  backend:
    port: 8000
`)
	_, err := ParseConfig(yaml)
	if err == nil {
		t.Fatal("expected error for service missing path")
	}
}

func TestParseConfig_ServiceMissingPort(t *testing.T) {
	yaml := []byte(`
version: 1
name: myproject
services:
  backend:
    path: apps/backend
`)
	_, err := ParseConfig(yaml)
	if err == nil {
		t.Fatal("expected error for service missing port")
	}
}

func TestParseConfig_DatabaseMissingEngine(t *testing.T) {
	yaml := []byte(`
version: 1
name: myproject
core:
  databases:
    main:
      local:
        image: postgres:16
        port: 5500
services:
  backend:
    path: apps/backend
    port: 8000
`)
	_, err := ParseConfig(yaml)
	if err == nil {
		t.Fatal("expected error for database missing engine")
	}
}

func TestParseConfig_OptionalSeed(t *testing.T) {
	yaml := []byte(`
version: 1
name: myproject
core:
  databases:
    main:
      engine: postgres
      local:
        image: postgres:16
        port: 5500
        user: postgres
        password: postgres
        template_db: dev_template
services:
  backend:
    path: apps/backend
    port: 8000
`)
	cfg, err := ParseConfig(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Core.Databases["main"].Local.Seed) != 0 {
		t.Error("expected empty seed config")
	}
}

func TestParseConfig_WithSeed(t *testing.T) {
	yaml := []byte(`
version: 1
name: myproject
core:
  databases:
    main:
      engine: postgres
      local:
        image: postgres:16
        port: 5500
        user: postgres
        password: postgres
        template_db: dev_template
        seed:
          - sql: schema.sql
          - s3:
              bucket: my-snapshots
              key: dev/dump.sql
              region: us-east-1
services:
  backend:
    path: apps/backend
    port: 8000
`)
	cfg, err := ParseConfig(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	seed := cfg.Core.Databases["main"].Local.Seed
	if len(seed) != 2 {
		t.Fatalf("expected 2 seed steps, got %d", len(seed))
	}
	if seed[0].SQL != "schema.sql" {
		t.Errorf("expected first step sql 'schema.sql', got '%s'", seed[0].SQL)
	}
	if seed[1].S3 == nil || seed[1].S3.Bucket != "my-snapshots" {
		t.Errorf("expected second step s3 bucket 'my-snapshots'")
	}
}

func TestAllBasePorts(t *testing.T) {
	cfg := &ProjectConfig{
		Services: map[string]ServiceConfig{
			"backend": {Path: "apps/backend", Port: 8000},
			"web":     {Path: "apps/web", Port: 3000},
		},
		InfraServices: map[string]InfraService{
			"redis": {Name: "redis", Image: "redis:7-alpine", Port: 6379},
		},
	}

	ports := cfg.AllBasePorts()
	if ports["backend"] != 8000 {
		t.Errorf("expected backend port 8000, got %d", ports["backend"])
	}
	if ports["web"] != 3000 {
		t.Errorf("expected web port 3000, got %d", ports["web"])
	}
	if ports["redis"] != 6379 {
		t.Errorf("expected redis port 6379, got %d", ports["redis"])
	}
	if len(ports) != 3 {
		t.Errorf("expected 3 ports, got %d", len(ports))
	}
}
