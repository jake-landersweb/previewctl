package domain

import (
	"testing"
)

func TestParseConfig_Valid(t *testing.T) {
	yaml := []byte(`
version: 1
name: myproject

provisioner:
  services:
    main:
      outputs: [connection_string, host, port, user, password, database]
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
`)

	cfg, err := ParseConfig(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Name != "myproject" {
		t.Errorf("expected name 'myproject', got '%s'", cfg.Name)
	}
	if len(cfg.Provisioner.Services) != 1 {
		t.Fatalf("expected 1 provisioner service, got %d", len(cfg.Provisioner.Services))
	}
	svc := cfg.Provisioner.Services["main"]
	if len(svc.Outputs) != 6 {
		t.Errorf("expected 6 outputs, got %d", len(svc.Outputs))
	}
	if svc.Init != "./scripts/init-db.sh" {
		t.Errorf("expected init hook, got '%s'", svc.Init)
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
provisioner:
  services:
    main:
      init: ./init.sh
services:
  backend:
    path: apps/backend
`)
	_, err := ParseConfig(yaml)
	if err == nil {
		t.Fatal("expected error for provisioner service missing outputs")
	}
}

func TestParseConfig_RunnerBuild(t *testing.T) {
	yaml := []byte(`
version: 1
name: myproject
runner:
  build: pnpm turbo build
services:
  backend:
    path: apps/backend
`)
	cfg, err := ParseConfig(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Runner == nil {
		t.Fatal("expected runner config")
	}
	if cfg.Runner.Build.Command != "pnpm turbo build" {
		t.Errorf("expected runner.build 'pnpm turbo build', got '%s'", cfg.Runner.Build.Command)
	}
	if !cfg.Runner.Build.CacheAllowed() {
		t.Error("expected string runner.build to allow cache by default")
	}
}

func TestParseConfig_RunnerHookObject(t *testing.T) {
	yaml := []byte(`
version: 1
name: myproject
runner:
  after:
    command: cd apps/backend && pnpm migration:run
    allow_cache: false
services:
  backend:
    path: apps/backend
`)
	cfg, err := ParseConfig(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Runner == nil {
		t.Fatal("expected runner config")
	}
	if cfg.Runner.After.Command != "cd apps/backend && pnpm migration:run" {
		t.Errorf("expected runner.after command, got '%s'", cfg.Runner.After.Command)
	}
	if cfg.Runner.After.AllowCache == nil {
		t.Fatal("expected allow_cache to be parsed")
	}
	if cfg.Runner.After.CacheAllowed() {
		t.Error("expected runner.after allow_cache=false to disable cache")
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
	if len(cfg.Provisioner.Services) != 0 {
		t.Error("expected no provisioner services")
	}
}
