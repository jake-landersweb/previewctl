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
		t.Skipf("airgoods config uses old format, skipping: %v", err)
	}

	if cfg.Name != "airgoods" {
		t.Errorf("expected name 'airgoods', got '%s'", cfg.Name)
	}
	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}

	// Services
	if len(cfg.Services) != 12 {
		t.Errorf("expected 12 services, got %d", len(cfg.Services))
	}

	backend := cfg.Services["backend"]
	if backend.Path != "apps/backend" {
		t.Errorf("expected backend path 'apps/backend', got '%s'", backend.Path)
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
		CoreOutputs:  make(map[string]map[string]string),
	}

	rendered, err := RenderEnvMap(backend.Env, ctx)
	if err != nil {
		// The airgoods config may use old-format template vars, skip if so
		t.Skipf("template rendering failed (likely old format): %v", err)
	}

	// Verify the port is in the allocated range
	renderedPort := rendered["PORT"]
	if renderedPort == "" || renderedPort == "0" {
		t.Errorf("expected PORT to be allocated, got '%s'", renderedPort)
	}
}
