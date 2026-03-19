package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jake-landersweb/previewctl/src/domain"
)

func TestEnvGenAdapter_Generate(t *testing.T) {
	workdir := t.TempDir()

	// Create service directories
	_ = os.MkdirAll(filepath.Join(workdir, "apps/backend"), 0o755)
	_ = os.MkdirAll(filepath.Join(workdir, "apps/web"), 0o755)

	cfg := &domain.ProjectConfig{
		Services: map[string]domain.ServiceConfig{
			"backend": {
				Path: "apps/backend",
					Env: map[string]string{
					"PORT":         "{{services.backend.port}}",
					"DATABASE_URL": "{{core.databases.main.connection_string}}",
				},
			},
			"web": {
				Path: "apps/web",
					Env: map[string]string{
					"PORT":    "{{services.web.port}}",
					"API_URL": "http://localhost:{{services.backend.port}}",
				},
			},
		},
	}

	adapter := NewEnvGenAdapter(cfg)
	ctx := context.Background()
	ports := domain.PortMap{"backend": 8042, "web": 3042}
	databases := map[string]*domain.DatabaseInfo{
		"main": {
			Host:             "localhost",
			Port:             5500,
			User:             "postgres",
			Password:         "secret",
			Database:         "wt_test",
			ConnectionString: "postgresql://postgres:secret@localhost:5500/wt_test",
		},
	}

	err := adapter.Generate(ctx, "test-env", workdir, ports, databases)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check backend .env.local
	backendEnv, err := os.ReadFile(filepath.Join(workdir, "apps/backend/.env.local"))
	if err != nil {
		t.Fatalf("reading backend .env.local: %v", err)
	}
	content := string(backendEnv)
	if !strings.Contains(content, "PORT=8042") {
		t.Error("expected PORT=8042 in backend .env.local")
	}
	if !strings.Contains(content, "DATABASE_URL=postgresql://postgres:secret@localhost:5500/wt_test") {
		t.Error("expected DATABASE_URL in backend .env.local")
	}

	// Check web .env.local
	webEnv, err := os.ReadFile(filepath.Join(workdir, "apps/web/.env.local"))
	if err != nil {
		t.Fatalf("reading web .env.local: %v", err)
	}
	webContent := string(webEnv)
	if !strings.Contains(webContent, "PORT=3042") {
		t.Error("expected PORT=3042 in web .env.local")
	}
	if !strings.Contains(webContent, "API_URL=http://localhost:8042") {
		t.Error("expected API_URL=http://localhost:8042 in web .env.local")
	}
}

func TestEnvGenAdapter_Generate_SharedPath(t *testing.T) {
	workdir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(workdir, "apps/backend"), 0o755)

	// Two services sharing the same path (like backend + queue in the POC)
	cfg := &domain.ProjectConfig{
		Services: map[string]domain.ServiceConfig{
			"backend": {
				Path: "apps/backend",
					Env:  map[string]string{"PORT": "{{services.backend.port}}"},
			},
			"queue": {
				Path: "apps/backend",
					Env:  map[string]string{"QUEUE_PORT": "{{services.queue.port}}"},
			},
		},
	}

	adapter := NewEnvGenAdapter(cfg)
	ports := domain.PortMap{"backend": 8042, "queue": 8043}

	err := adapter.Generate(context.Background(), "test-env", workdir, ports, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(workdir, "apps/backend/.env.local"))
	if err != nil {
		t.Fatalf("reading .env.local: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "PORT=8042") {
		t.Error("expected PORT=8042")
	}
	if !strings.Contains(s, "QUEUE_PORT=8043") {
		t.Error("expected QUEUE_PORT=8043")
	}
}

func TestEnvGenAdapter_Cleanup(t *testing.T) {
	workdir := t.TempDir()
	envPath := filepath.Join(workdir, "apps/backend/.env.local")
	_ = os.MkdirAll(filepath.Dir(envPath), 0o755)
	_ = os.WriteFile(envPath, []byte("PORT=8042\n"), 0o644)

	cfg := &domain.ProjectConfig{
		Services: map[string]domain.ServiceConfig{
			"backend": {Path: "apps/backend"},
		},
	}

	adapter := NewEnvGenAdapter(cfg)
	err := adapter.Cleanup(context.Background(), workdir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Error("expected .env.local to be removed")
	}
}

func TestEnvGenAdapter_Generate_NoEnv(t *testing.T) {
	workdir := t.TempDir()

	cfg := &domain.ProjectConfig{
		Services: map[string]domain.ServiceConfig{
			"backend": {Path: "apps/backend"},
		},
	}

	adapter := NewEnvGenAdapter(cfg)
	err := adapter.Generate(context.Background(), "test-env", workdir, domain.PortMap{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No .env.local should be created when service has no env config
	envPath := filepath.Join(workdir, "apps/backend/.env.local")
	if _, err := os.Stat(envPath); !os.IsNotExist(err) {
		t.Error("expected no .env.local when service has no env config")
	}
}
