package domain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfig_Airgoods(t *testing.T) {
	// Try the active worktree first, fall back to the feature branch
	paths := []string{
		"/Users/jake/worktrees/airgoods/airgoods/pv-test-21/previewctl.yaml",
		"/Users/jake/worktrees/airgoods/feat/isolated-local-dev/previewctl.yaml",
	}

	var data []byte
	var configDir string
	for _, p := range paths {
		d, err := os.ReadFile(p)
		if err == nil {
			data = d
			configDir = filepath.Dir(p)
			break
		}
	}
	if data == nil {
		t.Skip("airgoods config not found at any known path")
	}

	cfg, err := ParseConfig(data)
	if err != nil {
		t.Skipf("airgoods config parse error (may be old format): %v", err)
	}

	// Basic config
	if cfg.Name != "airgoods" {
		t.Errorf("expected name 'airgoods', got '%s'", cfg.Name)
	}
	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}

	// Provisioner services
	if len(cfg.Provisioner.Services) == 0 {
		t.Fatal("expected at least one provisioner service")
	}
	postgres, ok := cfg.Provisioner.Services["postgres"]
	if !ok {
		t.Fatal("expected 'postgres' provisioner service")
	}
	if len(postgres.Outputs) == 0 {
		t.Error("expected postgres outputs to be declared")
	}

	// Application services
	if len(cfg.Services) < 10 {
		t.Errorf("expected at least 10 services, got %d", len(cfg.Services))
	}

	backend := cfg.Services["backend"]
	if backend.Path != "apps/backend" {
		t.Errorf("expected backend path 'apps/backend', got '%s'", backend.Path)
	}

	// Overlay loading
	overlayPath := filepath.Join(configDir, "previewctl.local.yaml")
	if _, err := os.Stat(overlayPath); err == nil {
		overlayCfg, err := LoadConfigWithOverlay(filepath.Join(configDir, "previewctl.yaml"), "local")
		if err != nil {
			t.Fatalf("overlay loading failed: %v", err)
		}

		// Verify hooks were merged from overlay
		pgSvc := overlayCfg.Provisioner.Services["postgres"]
		if pgSvc.Seed == "" {
			t.Error("expected seed hook from overlay")
		}
		if pgSvc.Destroy == "" {
			t.Error("expected destroy hook from overlay")
		}

		// Outputs preserved from base
		if len(pgSvc.Outputs) == 0 {
			t.Error("expected outputs preserved from base after overlay merge")
		}
	}

	// Simulate infra services (normally populated from compose file parsing)
	cfg.InfraServices = map[string]InfraService{
		"redis": {Name: "redis", Port: 6379},
	}

	// Port allocation
	serviceNames := cfg.ServiceNames()
	if len(serviceNames) < 10 {
		t.Errorf("expected at least 10 service names, got %d", len(serviceNames))
	}

	ports, err := AllocatePortBlock("feat-auth", serviceNames)
	if err != nil {
		t.Fatalf("failed to allocate ports: %v", err)
	}

	for name, port := range ports {
		if port < 61000 || port >= 65000 {
			t.Errorf("port for %s out of range: %d", name, port)
		}
	}

	// Template rendering
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
		ProvisionerOutputs: map[string]map[string]string{
			"postgres": {
				"CONNECTION_STRING": "postgresql://test:test@localhost:5432/test",
				"DB_HOST":           "localhost",
				"DB_PORT":           "5432",
				"DB_USER":           "test",
				"DB_PASSWORD":       "test",
				"DB_NAME":           "test",
			},
		},
	}

	ctx.CurrentService = "backend"
	rendered, err := RenderEnvMap(backend.Env, ctx)
	if err != nil {
		t.Fatalf("template rendering failed: %v", err)
	}

	// Verify self.port resolved
	if rendered["PORT"] == "" || rendered["PORT"] == "0" {
		t.Errorf("expected PORT to be allocated, got '%s'", rendered["PORT"])
	}

	// Verify provisioner outputs resolved
	if rendered["DB_HOST_LOCAL"] != "localhost" {
		t.Errorf("expected DB_HOST_LOCAL=localhost, got '%s'", rendered["DB_HOST_LOCAL"])
	}

	// Verify cross-service port reference
	if rendered["SITE_URL_LOCAL"] == "" {
		t.Error("expected SITE_URL_LOCAL to be rendered")
	}
}
