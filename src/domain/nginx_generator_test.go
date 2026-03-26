package domain

import (
	"strings"
	"testing"
)

func TestGenerateNginxConfig_Basic(t *testing.T) {
	cfg := &ProjectConfig{
		Name: "testproject",
		Services: map[string]ServiceConfig{
			"backend": {Path: "apps/backend", Start: "node index.js"},
			"web":     {Path: "apps/web", Start: "npx vite preview"},
		},
	}

	manifest := &Manifest{
		EnvName: "test-env",
		Ports:   PortMap{"backend": 8000, "web": 3000, "redis": 6379},
	}

	data, err := GenerateNginxConfig(cfg, manifest, "preview.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := string(data)

	// Should have map block
	if !strings.Contains(content, "map $service $upstream_port") {
		t.Error("expected map block")
	}
	if !strings.Contains(content, "backend 8000;") {
		t.Error("expected backend port in map")
	}
	if !strings.Contains(content, "web 3000;") {
		t.Error("expected web port in map")
	}

	// Should have generic server block with regex
	if !strings.Contains(content, "server_name ~^.+--.+\\.preview\\.example\\.com$") {
		t.Error("expected generic server_name regex")
	}

	// Should have health check
	if !strings.Contains(content, "location /health") {
		t.Error("expected health check location")
	}

	// Should proxy to localhost
	if !strings.Contains(content, "proxy_pass http://127.0.0.1:$upstream_port") {
		t.Error("expected proxy_pass with upstream_port variable")
	}
}

func TestGenerateNginxConfig_WithProxy(t *testing.T) {
	cfg := &ProjectConfig{
		Name: "testproject",
		Services: map[string]ServiceConfig{
			"backend": {Path: "apps/backend", Start: "node index.js"},
			"web": {
				Path:  "apps/web",
				Start: "npx vite preview",
				Proxy: &ServiceProxy{
					Path: "/api",
					To:   ServiceProxyTarget{Service: "backend"},
				},
			},
			"dashboard": {
				Path:  "apps/dashboard",
				Start: "npx next start",
				Proxy: &ServiceProxy{
					Path: "/api",
					To:   ServiceProxyTarget{Service: "backend"},
				},
			},
		},
	}

	manifest := &Manifest{
		EnvName: "my-env",
		Ports:   PortMap{"backend": 8000, "web": 3000, "dashboard": 4000},
	}

	data, err := GenerateNginxConfig(cfg, manifest, "preview.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := string(data)

	// Should have dedicated server block for web
	if !strings.Contains(content, "server_name my-env--web.preview.example.com;") {
		t.Error("expected web-specific server_name")
	}

	// Should have /api/ proxy to backend port with passthrough
	if !strings.Contains(content, "location /api/") {
		t.Error("expected /api/ location block")
	}
	if !strings.Contains(content, "proxy_pass http://127.0.0.1:8000/api/;") {
		t.Error("expected proxy_pass to backend port 8000 with /api/ path")
	}

	// Should also proxy / to web port
	if !strings.Contains(content, "proxy_pass http://127.0.0.1:3000;") {
		t.Error("expected proxy_pass to web port 3000")
	}

	// Should have dedicated server block for dashboard too
	if !strings.Contains(content, "server_name my-env--dashboard.preview.example.com;") {
		t.Error("expected dashboard-specific server_name")
	}
	if !strings.Contains(content, "proxy_pass http://127.0.0.1:4000;") {
		t.Error("expected proxy_pass to dashboard port 4000")
	}

	// Should set Host to localhost (for dev server allowedHosts)
	if !strings.Contains(content, "proxy_set_header Host localhost;") {
		t.Error("expected Host header set to localhost")
	}

	// Should set X-Forwarded-Host to original host
	if !strings.Contains(content, "proxy_set_header X-Forwarded-Host $host;") {
		t.Error("expected X-Forwarded-Host header")
	}
}

func TestGenerateNginxConfig_ProxyTargetNotFound(t *testing.T) {
	cfg := &ProjectConfig{
		Name: "testproject",
		Services: map[string]ServiceConfig{
			"web": {
				Path:  "apps/web",
				Start: "npx vite preview",
				Proxy: &ServiceProxy{
					Path: "/api",
					To:   ServiceProxyTarget{Service: "nonexistent"},
				},
			},
		},
	}

	manifest := &Manifest{
		EnvName: "test-env",
		Ports:   PortMap{"web": 3000},
	}

	_, err := GenerateNginxConfig(cfg, manifest, "preview.example.com")
	if err == nil {
		t.Fatal("expected error for missing proxy target service")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("expected error to mention missing service, got: %v", err)
	}
}

func TestGenerateNginxConfig_DomainEscaping(t *testing.T) {
	cfg := &ProjectConfig{
		Name:     "testproject",
		Services: map[string]ServiceConfig{},
	}

	manifest := &Manifest{
		EnvName: "test-env",
		Ports:   PortMap{"backend": 8000},
	}

	data, err := GenerateNginxConfig(cfg, manifest, "preview.my-domain.co.uk")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := string(data)

	// Dots in domain should be escaped in regex
	if !strings.Contains(content, "preview\\.my-domain\\.co\\.uk$") {
		t.Error("expected escaped dots in domain regex")
	}
}

func TestGenerateNginxConfig_ProxyWithTargetPath(t *testing.T) {
	cfg := &ProjectConfig{
		Name: "testproject",
		Services: map[string]ServiceConfig{
			"backend": {Path: "apps/backend", Start: "node index.js"},
			"dashboard": {
				Path:  "apps/dashboard",
				Start: "npx next start",
				Proxy: &ServiceProxy{
					Path:       "/iapi",
					TargetPath: "/api",
					To:         ServiceProxyTarget{Service: "backend"},
				},
			},
		},
	}

	manifest := &Manifest{
		EnvName: "my-env",
		Ports:   PortMap{"backend": 8000, "dashboard": 4000},
	}

	data, err := GenerateNginxConfig(cfg, manifest, "preview.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := string(data)

	// Should have /iapi/ location that rewrites to /api/ on backend
	if !strings.Contains(content, "location /iapi/") {
		t.Error("expected /iapi/ location block")
	}
	if !strings.Contains(content, "proxy_pass http://127.0.0.1:8000/api/;") {
		t.Error("expected proxy_pass with /api/ rewrite to backend")
	}
}

func TestGenerateNginxConfig_ProxyPassthrough(t *testing.T) {
	cfg := &ProjectConfig{
		Name: "testproject",
		Services: map[string]ServiceConfig{
			"backend": {Path: "apps/backend", Start: "node index.js"},
			"web": {
				Path:  "apps/web",
				Start: "npx vite preview",
				Proxy: &ServiceProxy{
					Path: "/api",
					To:   ServiceProxyTarget{Service: "backend"},
				},
			},
		},
	}

	manifest := &Manifest{
		EnvName: "my-env",
		Ports:   PortMap{"backend": 8000, "web": 3000},
	}

	data, err := GenerateNginxConfig(cfg, manifest, "preview.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := string(data)

	// When target_path is not set, it defaults to path — passthrough
	if !strings.Contains(content, "location /api/") {
		t.Error("expected /api/ location block")
	}
	if !strings.Contains(content, "proxy_pass http://127.0.0.1:8000/api/;") {
		t.Error("expected proxy_pass with /api/ passthrough")
	}
}

func TestGenerateNginxConfig_DeterministicOrder(t *testing.T) {
	cfg := &ProjectConfig{
		Name: "testproject",
		Services: map[string]ServiceConfig{
			"zeta":  {Path: "apps/zeta", Start: "start", Proxy: &ServiceProxy{Path: "/api", To: ServiceProxyTarget{Service: "alpha"}}},
			"alpha": {Path: "apps/alpha", Start: "start"},
		},
	}

	manifest := &Manifest{
		EnvName: "test-env",
		Ports:   PortMap{"alpha": 8000, "zeta": 8001},
	}

	data1, _ := GenerateNginxConfig(cfg, manifest, "example.com")
	data2, _ := GenerateNginxConfig(cfg, manifest, "example.com")

	if string(data1) != string(data2) {
		t.Error("expected deterministic output")
	}
}
