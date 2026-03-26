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

	// Should have per-service server blocks
	if !strings.Contains(content, "server_name test-env--backend.preview.example.com;") {
		t.Error("expected backend server_name")
	}
	if !strings.Contains(content, "server_name test-env--web.preview.example.com;") {
		t.Error("expected web server_name")
	}
	if !strings.Contains(content, "server_name test-env--redis.preview.example.com;") {
		t.Error("expected redis server_name")
	}

	// Should have health check
	if !strings.Contains(content, "location /health") {
		t.Error("expected health check location")
	}

	// Should proxy to correct ports
	if !strings.Contains(content, "proxy_pass http://127.0.0.1:8000;") {
		t.Error("expected backend proxy_pass")
	}
	if !strings.Contains(content, "proxy_pass http://127.0.0.1:3000;") {
		t.Error("expected web proxy_pass")
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
				Proxy: []ServiceProxy{{
					Path: "/api",
					To:   ServiceProxyTarget{Service: "backend"},
				}},
			},
			"dashboard": {
				Path:  "apps/dashboard",
				Start: "npx next start",
				Proxy: []ServiceProxy{{
					Path: "/api",
					To:   ServiceProxyTarget{Service: "backend"},
				}},
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

	// Should have dedicated server block for web with /api proxy
	if !strings.Contains(content, "server_name my-env--web.preview.example.com;") {
		t.Error("expected web-specific server_name")
	}
	if !strings.Contains(content, "location /api/") {
		t.Error("expected /api/ location block")
	}
	if !strings.Contains(content, "proxy_pass http://127.0.0.1:8000/api/;") {
		t.Error("expected proxy_pass to backend with /api/ path")
	}

	// Should also have default location for web
	if !strings.Contains(content, "proxy_pass http://127.0.0.1:3000;") {
		t.Error("expected web default proxy_pass")
	}

	// Should set Host to localhost
	if !strings.Contains(content, "proxy_set_header Host localhost;") {
		t.Error("expected Host header set to localhost")
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
				Proxy: []ServiceProxy{{
					Path:       "/iapi",
					TargetPath: "/api",
					To:         ServiceProxyTarget{Service: "backend"},
				}},
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
				Proxy: []ServiceProxy{{
					Path: "/api",
					To:   ServiceProxyTarget{Service: "backend"},
				}},
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

	if !strings.Contains(content, "location /api/") {
		t.Error("expected /api/ location block")
	}
	if !strings.Contains(content, "proxy_pass http://127.0.0.1:8000/api/;") {
		t.Error("expected proxy_pass with /api/ passthrough")
	}
}

func TestGenerateNginxConfig_ProxyTargetNotFound(t *testing.T) {
	cfg := &ProjectConfig{
		Name: "testproject",
		Services: map[string]ServiceConfig{
			"web": {
				Path:  "apps/web",
				Start: "npx vite preview",
				Proxy: []ServiceProxy{{
					Path: "/api",
					To:   ServiceProxyTarget{Service: "nonexistent"},
				}},
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

func TestGenerateNginxConfig_DeterministicOrder(t *testing.T) {
	cfg := &ProjectConfig{
		Name: "testproject",
		Services: map[string]ServiceConfig{
			"zeta":  {Path: "apps/zeta", Start: "start"},
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

	// Alpha should come before zeta
	content := string(data1)
	alphaIdx := strings.Index(content, "test-env--alpha")
	zetaIdx := strings.Index(content, "test-env--zeta")
	if alphaIdx > zetaIdx {
		t.Error("expected alpha before zeta")
	}
}
