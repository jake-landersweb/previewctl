package domain

import (
	"testing"
)

func TestParseConfig_Valid(t *testing.T) {
	yaml := []byte(`
version: 1
name: myproject

core:
  services:
    main:
      outputs: [connection_string, host, port, user, password, database]
      hooks:
        init: ./scripts/init-db.sh
        seed: ./scripts/seed-db.sh

services:
  backend:
    path: apps/backend
    command: pnpm dev
    depends_on: [redis]
    env:
      PORT: "{{services.backend.port}}"
  web:
    path: apps/web
    depends_on: [backend]
    env:
      PORT: "{{services.web.port}}"

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
	if len(cfg.Core.Services) != 1 {
		t.Fatalf("expected 1 core service, got %d", len(cfg.Core.Services))
	}
	svc := cfg.Core.Services["main"]
	if len(svc.Outputs) != 6 {
		t.Errorf("expected 6 outputs, got %d", len(svc.Outputs))
	}
	if svc.Hooks == nil {
		t.Fatal("expected hooks config")
	}
	if svc.Hooks.Init != "./scripts/init-db.sh" {
		t.Errorf("expected init hook, got '%s'", svc.Hooks.Init)
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
    command: pnpm dev
`)
	_, err := ParseConfig(yaml)
	if err == nil {
		t.Fatal("expected error for service missing path")
	}
}

func TestParseConfig_CoreServiceMissingOutputs(t *testing.T) {
	yaml := []byte(`
version: 1
name: myproject
core:
  services:
    main:
      hooks:
        init: ./init.sh
services:
  backend:
    path: apps/backend
`)
	_, err := ParseConfig(yaml)
	if err == nil {
		t.Fatal("expected error for core service missing outputs")
	}
}

func TestParseConfig_NoCoreServices(t *testing.T) {
	yaml := []byte(`
version: 1
name: myproject
services:
  backend:
    path: apps/backend
`)
	cfg, err := ParseConfig(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Core.Services) != 0 {
		t.Error("expected no core services")
	}
}
