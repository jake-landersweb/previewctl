package domain

import (
	"strings"
	"testing"
)

func TestGenerateComposeFile_Basic(t *testing.T) {
	cfg := &ProjectConfig{
		Name: "testproject",
		Services: map[string]ServiceConfig{
			"backend": {
				Path:  "apps/backend",
				Start: "node build/index.js",
			},
			"web": {
				Path:  "apps/web",
				Start: "npx vite preview",
			},
			"types": {
				Path: "packages/types",
				// No Start — should be skipped
			},
		},
		Runner: &RunnerConfig{
			Compose: &ComposeConfig{
				Autostart: []string{"backend", "web"},
				Image:     "node:20",
				Proxy: &ProxyConfig{
					Domain: "preview.example.com",
				},
			},
		},
	}

	manifest := &Manifest{
		EnvName: "test-env",
		Ports:   PortMap{"backend": 8000, "web": 3000, "redis": 6379},
	}

	data, err := GenerateComposeFile(cfg, manifest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := string(data)

	// Should include nginx
	if !strings.Contains(content, "nginx:") {
		t.Error("expected nginx service in output")
	}
	if !strings.Contains(content, "image: nginx:alpine") {
		t.Error("expected nginx:alpine image")
	}

	// Should include backend and web
	if !strings.Contains(content, "backend:") {
		t.Error("expected backend service in output")
	}
	if !strings.Contains(content, "web:") {
		t.Error("expected web service in output")
	}

	// Should NOT include types (no Start command)
	if strings.Contains(content, "types:") {
		t.Error("expected types to be excluded (no start command)")
	}

	// Should use correct image
	if !strings.Contains(content, "image: node:20") {
		t.Error("expected node:20 image for app services")
	}

	// Should have correct working dirs
	if !strings.Contains(content, "working_dir: /app/apps/backend") {
		t.Error("expected backend working_dir")
	}
	if !strings.Contains(content, "working_dir: /app/apps/web") {
		t.Error("expected web working_dir")
	}

	// Should include env_file with both .env and .env.local
	if !strings.Contains(content, "apps/backend/.env\n") {
		t.Error("expected backend .env in env_file")
	}
	if !strings.Contains(content, "apps/backend/.env.local") {
		t.Error("expected backend .env.local in env_file")
	}

	// Should depend on nginx
	if !strings.Contains(content, "condition: service_healthy") {
		t.Error("expected nginx health dependency")
	}
}

func TestGenerateComposeFile_ProxyDisabled(t *testing.T) {
	enabled := false
	cfg := &ProjectConfig{
		Name: "testproject",
		Services: map[string]ServiceConfig{
			"backend": {
				Path:  "apps/backend",
				Start: "node build/index.js",
			},
		},
		Runner: &RunnerConfig{
			Compose: &ComposeConfig{
				Autostart: []string{"backend"},
				Image:     "node:20",
				Proxy: &ProxyConfig{
					Enabled: &enabled,
					Domain:  "preview.example.com",
				},
			},
		},
	}

	manifest := &Manifest{
		EnvName: "test-env",
		Ports:   PortMap{"backend": 8000},
	}

	data, err := GenerateComposeFile(cfg, manifest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := string(data)

	// Should NOT include nginx
	if strings.Contains(content, "nginx:") {
		t.Error("expected nginx to be excluded when proxy is disabled")
	}

	// Should NOT have depends_on nginx
	if strings.Contains(content, "condition: service_healthy") {
		t.Error("expected no nginx dependency when proxy is disabled")
	}

	// Should still include backend
	if !strings.Contains(content, "backend:") {
		t.Error("expected backend service in output")
	}
}

func TestGenerateComposeFile_NoComposeConfig(t *testing.T) {
	cfg := &ProjectConfig{
		Name:   "testproject",
		Runner: &RunnerConfig{},
	}

	_, err := GenerateComposeFile(cfg, &Manifest{})
	if err == nil {
		t.Fatal("expected error when runner.compose is nil")
	}
}

func TestGenerateComposeFile_NoImage(t *testing.T) {
	cfg := &ProjectConfig{
		Name: "testproject",
		Runner: &RunnerConfig{
			Compose: &ComposeConfig{
				Autostart: []string{"backend"},
				// Image is empty
			},
		},
	}

	_, err := GenerateComposeFile(cfg, &Manifest{})
	if err == nil {
		t.Fatal("expected error when image is empty")
	}
}

func TestGenerateComposeFile_CustomEnvFile(t *testing.T) {
	cfg := &ProjectConfig{
		Name: "testproject",
		Services: map[string]ServiceConfig{
			"backend": {
				Path:    "apps/backend",
				Start:   "node build/index.js",
				EnvFile: ".env.preview",
			},
		},
		Runner: &RunnerConfig{
			Compose: &ComposeConfig{
				Autostart: []string{"backend"},
				Image:     "node:20",
				Proxy: &ProxyConfig{
					Domain: "preview.example.com",
				},
			},
		},
	}

	manifest := &Manifest{
		EnvName: "test-env",
		Ports:   PortMap{"backend": 8000},
	}

	data, err := GenerateComposeFile(cfg, manifest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "apps/backend/.env.preview") {
		t.Error("expected custom env file path")
	}
}

func TestGenerateComposeFile_DeterministicOrder(t *testing.T) {
	cfg := &ProjectConfig{
		Name: "testproject",
		Services: map[string]ServiceConfig{
			"zeta":  {Path: "apps/zeta", Start: "start-zeta"},
			"alpha": {Path: "apps/alpha", Start: "start-alpha"},
			"mid":   {Path: "apps/mid", Start: "start-mid"},
		},
		Runner: &RunnerConfig{
			Compose: &ComposeConfig{
				Autostart: []string{"alpha", "zeta"},
				Image:     "node:20",
				Proxy: &ProxyConfig{
					Domain: "preview.example.com",
				},
			},
		},
	}

	manifest := &Manifest{
		EnvName: "test-env",
		Ports:   PortMap{"alpha": 8000, "mid": 8001, "zeta": 8002},
	}

	// Generate twice — should be identical
	data1, err := GenerateComposeFile(cfg, manifest)
	if err != nil {
		t.Fatalf("first generation error: %v", err)
	}
	data2, err := GenerateComposeFile(cfg, manifest)
	if err != nil {
		t.Fatalf("second generation error: %v", err)
	}

	if string(data1) != string(data2) {
		t.Error("expected deterministic output")
	}

	// Services should be in alphabetical order
	content := string(data1)
	alphaIdx := strings.Index(content, "  alpha:")
	midIdx := strings.Index(content, "  mid:")
	zetaIdx := strings.Index(content, "  zeta:")

	if alphaIdx > midIdx || midIdx > zetaIdx {
		t.Error("expected services in alphabetical order")
	}
}
