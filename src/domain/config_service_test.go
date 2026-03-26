package domain

import (
	"testing"
)

func TestParseConfig_BuildStartProxy(t *testing.T) {
	yaml := `
version: 1
name: test
services:
  backend:
    path: apps/backend
    build: pnpm turbo build --filter=backend
    start: node build/index.js
    env:
      PORT: "8000"
  web:
    path: apps/web
    build: pnpm turbo build --filter=web
    start: npx vite preview
    proxy:
      - path: /api
        to:
          service: backend
    env:
      PORT: "3000"
  types:
    path: packages/types
`

	cfg, err := ParseConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Backend
	backend := cfg.Services["backend"]
	if backend.Build != "pnpm turbo build --filter=backend" {
		t.Errorf("expected backend build command, got '%s'", backend.Build)
	}
	if backend.Start != "node build/index.js" {
		t.Errorf("expected backend start command, got '%s'", backend.Start)
	}
	if backend.Proxy != nil {
		t.Error("expected backend to have no proxy")
	}

	// Web
	web := cfg.Services["web"]
	if web.Build != "pnpm turbo build --filter=web" {
		t.Errorf("expected web build command, got '%s'", web.Build)
	}
	if web.Start != "npx vite preview" {
		t.Errorf("expected web start command, got '%s'", web.Start)
	}
	if len(web.Proxy) == 0 {
		t.Fatal("expected web to have proxy config")
	}
	if web.Proxy[0].Path != "/api" {
		t.Errorf("expected proxy path '/api', got '%s'", web.Proxy[0].Path)
	}
	if web.Proxy[0].To.Service != "backend" {
		t.Errorf("expected proxy target 'backend', got '%s'", web.Proxy[0].To.Service)
	}

	// Types — no build or start
	types := cfg.Services["types"]
	if types.Build != "" {
		t.Errorf("expected types to have no build, got '%s'", types.Build)
	}
	if types.Start != "" {
		t.Errorf("expected types to have no start, got '%s'", types.Start)
	}
}

func TestParseConfig_ComposeConfig(t *testing.T) {
	yaml := `
version: 1
name: test
services:
  backend:
    path: apps/backend
runner:
  before: ./setup.sh
  compose:
    autostart: [backend, web]
    image: node:20
    proxy:
      domain: preview.example.com
      type: nginx
`

	cfg, err := ParseConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if cfg.Runner.Compose == nil {
		t.Fatal("expected runner.compose to be set")
	}
	if len(cfg.Runner.Compose.Autostart) != 2 {
		t.Errorf("expected 2 autostart services, got %d", len(cfg.Runner.Compose.Autostart))
	}
	if cfg.Runner.Compose.Image != "node:20" {
		t.Errorf("expected image 'node:20', got '%s'", cfg.Runner.Compose.Image)
	}
	if cfg.Runner.Compose.Proxy == nil {
		t.Fatal("expected proxy config")
	}
	if cfg.Runner.Compose.Proxy.Domain != "preview.example.com" {
		t.Errorf("expected domain 'preview.example.com', got '%s'", cfg.Runner.Compose.Proxy.Domain)
	}
	if cfg.Runner.Compose.Proxy.ResolvedType() != "nginx" {
		t.Errorf("expected type 'nginx', got '%s'", cfg.Runner.Compose.Proxy.ResolvedType())
	}
	if !cfg.Runner.Compose.Proxy.IsEnabled() {
		t.Error("expected proxy to be enabled by default")
	}
}

func TestProxyConfig_Defaults(t *testing.T) {
	// nil proxy
	var p *ProxyConfig
	if p.IsEnabled() {
		t.Error("nil proxy should not be enabled")
	}
	if p.ResolvedType() != "nginx" {
		t.Error("nil proxy should default to nginx")
	}

	// Empty proxy (enabled is nil = true)
	p = &ProxyConfig{Domain: "example.com"}
	if !p.IsEnabled() {
		t.Error("proxy with nil enabled should default to true")
	}
	if p.ResolvedType() != "nginx" {
		t.Error("proxy with empty type should default to nginx")
	}

	// Explicitly disabled
	enabled := false
	p = &ProxyConfig{Enabled: &enabled}
	if p.IsEnabled() {
		t.Error("proxy with enabled=false should be disabled")
	}
}

func TestServiceProxy_ResolvedTargetPath(t *testing.T) {
	// Default: target_path equals path
	p := &ServiceProxy{Path: "/api", To: ServiceProxyTarget{Service: "backend"}}
	if p.ResolvedTargetPath() != "/api" {
		t.Errorf("expected '/api', got '%s'", p.ResolvedTargetPath())
	}

	// Custom target_path
	p = &ServiceProxy{Path: "/iapi", TargetPath: "/api", To: ServiceProxyTarget{Service: "backend"}}
	if p.ResolvedTargetPath() != "/api" {
		t.Errorf("expected '/api', got '%s'", p.ResolvedTargetPath())
	}
}

func TestDeepMergeConfig_ServiceBuildStartProxy(t *testing.T) {
	base := &ProjectConfig{
		Version: 1,
		Name:    "test",
		Services: map[string]ServiceConfig{
			"backend": {
				Path: "apps/backend",
				Env:  map[string]string{"PORT": "8000"},
			},
			"web": {
				Path: "apps/web",
				Env:  map[string]string{"PORT": "3000"},
			},
		},
	}

	overlay := &ProjectConfig{
		Services: map[string]ServiceConfig{
			"backend": {
				Build: "pnpm build",
				Start: "node index.js",
			},
			"web": {
				Build: "pnpm build",
				Start: "npx vite preview",
				Proxy: []ServiceProxy{{
					Path: "/api",
					To:   ServiceProxyTarget{Service: "backend"},
				}},
			},
		},
	}

	deepMergeConfig(base, overlay)

	// Backend should have new build/start but keep original path and env
	if base.Services["backend"].Build != "pnpm build" {
		t.Error("expected backend build to be set from overlay")
	}
	if base.Services["backend"].Start != "node index.js" {
		t.Error("expected backend start to be set from overlay")
	}
	if base.Services["backend"].Path != "apps/backend" {
		t.Error("expected backend path preserved from base")
	}
	if base.Services["backend"].Env["PORT"] != "8000" {
		t.Error("expected backend env preserved from base")
	}

	// Web should have proxy from overlay
	if len(base.Services["web"].Proxy) == 0 {
		t.Fatal("expected web proxy from overlay")
	}
	if base.Services["web"].Proxy[0].Path != "/api" {
		t.Error("expected web proxy path from overlay")
	}
}

func TestDeepMergeConfig_ComposeConfig(t *testing.T) {
	base := &ProjectConfig{
		Version: 1,
		Name:    "test",
		Runner: &RunnerConfig{
			Before: "./setup.sh",
			After:  "./migrate.sh",
		},
	}

	overlay := &ProjectConfig{
		Runner: &RunnerConfig{
			Compose: &ComposeConfig{
				Autostart: []string{"backend"},
				Image:     "node:20",
				Proxy:     &ProxyConfig{Domain: "preview.example.com"},
			},
		},
	}

	deepMergeConfig(base, overlay)

	// Runner hooks should be preserved
	if base.Runner.Before != "./setup.sh" {
		t.Error("expected runner.before preserved")
	}
	if base.Runner.After != "./migrate.sh" {
		t.Error("expected runner.after preserved")
	}

	// Compose should be added
	if base.Runner.Compose == nil {
		t.Fatal("expected runner.compose from overlay")
	}
	if base.Runner.Compose.Image != "node:20" {
		t.Error("expected compose image from overlay")
	}
	if base.Runner.Compose.Proxy.Domain != "preview.example.com" {
		t.Error("expected compose proxy domain from overlay")
	}
}
