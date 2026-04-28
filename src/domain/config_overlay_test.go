package domain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigWithOverlay_MergesHooksIntoBase(t *testing.T) {
	dir := t.TempDir()

	base := `
version: 1
name: myproject
provisioner:
  services:
    postgres:
      outputs: [CONNECTION_STRING, DB_HOST]
services:
  backend:
    path: apps/backend
`
	overlay := `
provisioner:
  services:
    postgres:
      init: ./scripts/init.sh
      seed: ./scripts/seed.sh
infrastructure:
  compose_file: compose.yaml
`
	_ = os.WriteFile(filepath.Join(dir, "previewctl.yaml"), []byte(base), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "previewctl.local.yaml"), []byte(overlay), 0o644)

	cfg, err := LoadConfigWithOverlay(filepath.Join(dir, "previewctl.yaml"), "local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	svc := cfg.Provisioner.Services["postgres"]

	// Base outputs preserved
	if len(svc.Outputs) != 2 {
		t.Errorf("expected 2 outputs preserved from base, got %d", len(svc.Outputs))
	}

	// Overlay hooks merged in
	if svc.Init != "./scripts/init.sh" {
		t.Errorf("expected init hook from overlay, got '%s'", svc.Init)
	}
	if svc.Seed != "./scripts/seed.sh" {
		t.Errorf("expected seed hook from overlay, got '%s'", svc.Seed)
	}

	// Overlay infrastructure merged
	if cfg.Infrastructure == nil || cfg.Infrastructure.ComposeFile != "compose.yaml" {
		t.Error("expected infrastructure from overlay")
	}

	// Mode set
	if cfg.Mode != "local" {
		t.Errorf("expected mode 'local', got '%s'", cfg.Mode)
	}
}

func TestLoadConfigWithOverlay_NoOverlayFile(t *testing.T) {
	dir := t.TempDir()

	base := `
version: 1
name: myproject
provisioner:
  services:
    postgres:
      outputs: [CONNECTION_STRING]
services:
  backend:
    path: apps/backend
`
	_ = os.WriteFile(filepath.Join(dir, "previewctl.yaml"), []byte(base), 0o644)

	cfg, err := LoadConfigWithOverlay(filepath.Join(dir, "previewctl.yaml"), "local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Provisioner.Services["postgres"].Outputs) != 1 {
		t.Error("expected base config unchanged when no overlay exists")
	}
}

func TestLoadConfigWithOverlay_OverlayOutputsReplace(t *testing.T) {
	dir := t.TempDir()

	base := `
version: 1
name: myproject
provisioner:
  services:
    postgres:
      outputs: [A, B]
services:
  backend:
    path: apps/backend
`
	overlay := `
provisioner:
  services:
    postgres:
      outputs: [X, Y, Z]
`
	_ = os.WriteFile(filepath.Join(dir, "previewctl.yaml"), []byte(base), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "previewctl.local.yaml"), []byte(overlay), 0o644)

	cfg, err := LoadConfigWithOverlay(filepath.Join(dir, "previewctl.yaml"), "local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outputs := cfg.Provisioner.Services["postgres"].Outputs
	if len(outputs) != 3 || outputs[0] != "X" {
		t.Errorf("expected overlay outputs to replace base, got %v", outputs)
	}
}

func TestLoadConfigWithOverlay_OverlayRunnerBuildOverridesBase(t *testing.T) {
	dir := t.TempDir()

	base := `
version: 1
name: myproject
provisioner:
  services:
    postgres:
      outputs: [DSN]
runner:
  before: ./scripts/base-before.sh
  build: pnpm turbo build --filter=base
services:
  backend:
    path: apps/backend
`
	overlay := `
runner:
  build: pnpm turbo build
`
	_ = os.WriteFile(filepath.Join(dir, "previewctl.yaml"), []byte(base), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "previewctl.local.yaml"), []byte(overlay), 0o644)

	cfg, err := LoadConfigWithOverlay(filepath.Join(dir, "previewctl.yaml"), "local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Runner == nil {
		t.Fatal("expected runner config")
	}
	if cfg.Runner.Build != "pnpm turbo build" {
		t.Errorf("expected overlay build to override base, got '%s'", cfg.Runner.Build)
	}
	// Sibling fields not in overlay must be preserved.
	if cfg.Runner.Before != "./scripts/base-before.sh" {
		t.Errorf("expected base before preserved, got '%s'", cfg.Runner.Before)
	}
}

func TestLoadConfigWithOverlay_OverlayAddsNewService(t *testing.T) {
	dir := t.TempDir()

	base := `
version: 1
name: myproject
provisioner:
  services:
    postgres:
      outputs: [DSN]
services:
  backend:
    path: apps/backend
`
	overlay := `
services:
  web:
    path: apps/web
`
	_ = os.WriteFile(filepath.Join(dir, "previewctl.yaml"), []byte(base), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "previewctl.local.yaml"), []byte(overlay), 0o644)

	cfg, err := LoadConfigWithOverlay(filepath.Join(dir, "previewctl.yaml"), "local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Services) != 2 {
		t.Errorf("expected 2 services (base + overlay), got %d", len(cfg.Services))
	}
	if _, ok := cfg.Services["web"]; !ok {
		t.Error("expected 'web' service from overlay")
	}
}
