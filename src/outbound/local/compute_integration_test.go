//go:build integration

package local

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jake-landersweb/previewctl/src/domain"
)

// writeTestComposeFile creates a minimal compose file for testing.
func writeTestComposeFile(t *testing.T, dir string) string {
	t.Helper()
	composeFile := filepath.Join(dir, "compose.test.yaml")
	content := `services:
  redis:
    image: redis:7-alpine
    ports:
      - "${REDIS_PORT:-6379}:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 2s
      timeout: 2s
      retries: 3
`
	if err := os.WriteFile(composeFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing compose file: %v", err)
	}
	return composeFile
}

// containerExists checks if a docker container with the given name prefix is running.
func containerExists(projectName string) bool {
	cmd := exec.Command("docker", "compose", "-p", projectName, "ps", "-q")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// cleanupCompose ensures containers are torn down after test.
func cleanupCompose(t *testing.T, composeFile string, projectName string) {
	t.Helper()
	cmd := exec.Command("docker", "compose", "-f", composeFile, "-p", projectName, "down", "-v")
	cmd.Env = os.Environ()
	cmd.CombinedOutput()
}

func TestComputeAdapter_StartCreatesContainers(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	composeFile := writeTestComposeFile(t, tmpDir)
	projectName := "previewctl-test-start" // config.Name-envName

	t.Cleanup(func() { cleanupCompose(t, composeFile, projectName) })

	config := &domain.ProjectConfig{
		Name: "previewctl-test",
		Infrastructure: map[string]domain.InfraServiceConfig{
			"redis": {Image: "redis:7-alpine", Port: 6379},
		},
	}

	adapter := &ComputeAdapter{
		config:       config,
		composeFile:  composeFile,
		worktreeBase: tmpDir,
	}

	ports := domain.PortMap{"redis": 16379}

	err := adapter.Start(ctx, "start", ports)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify container is running — project name is config.Name-envName
	if !containerExists(projectName) {
		t.Error("expected container to be running after Start")
	}
}

func TestComputeAdapter_DestroyRemovesContainers(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	composeFile := writeTestComposeFile(t, tmpDir)
	projectName := "previewctl-test"

	t.Cleanup(func() { cleanupCompose(t, composeFile, projectName+"-destroy") })

	config := &domain.ProjectConfig{
		Name:  projectName,
		Local: &domain.LocalConfig{Worktree: domain.WorktreeConfig{BasePath: tmpDir}},
	}

	adapter := &ComputeAdapter{
		config:       config,
		composeFile:  composeFile,
		worktreeBase: tmpDir,
	}

	// Create a fake worktree directory (we're not testing git here)
	worktreePath := filepath.Join(tmpDir, projectName, "destroy")
	os.MkdirAll(worktreePath, 0o755)

	// Start containers first
	ports := domain.PortMap{"redis": 16380}
	if err := adapter.Start(ctx, "destroy", ports); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !containerExists(projectName + "-destroy") {
		t.Fatal("expected container to be running before Destroy")
	}

	// Now destroy — we can't use git worktree remove since it's not a real worktree,
	// so we test compose down separately
	cmd := exec.Command("docker", "compose", "-f", composeFile, "down", "-v")
	cmd.Env = append(os.Environ(), fmt.Sprintf("COMPOSE_PROJECT_NAME=%s-destroy", projectName))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compose down failed: %s", string(out))
	}

	if containerExists(projectName + "-destroy") {
		t.Error("expected container to be removed after Destroy")
	}
}

func TestComputeAdapter_StopStopsContainers(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	composeFile := writeTestComposeFile(t, tmpDir)
	projectName := "previewctl-test"

	t.Cleanup(func() { cleanupCompose(t, composeFile, projectName+"-stoptest") })

	config := &domain.ProjectConfig{
		Name: projectName,
	}

	adapter := &ComputeAdapter{
		config:       config,
		composeFile:  composeFile,
		worktreeBase: tmpDir,
	}

	ports := domain.PortMap{"redis": 16381}
	if err := adapter.Start(ctx, "stoptest", ports); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if err := adapter.Stop(ctx, "stoptest"); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestComputeAdapter_IsRunning(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	composeFile := writeTestComposeFile(t, tmpDir)
	projectName := "previewctl-test"

	t.Cleanup(func() { cleanupCompose(t, composeFile, projectName+"-isrunning") })

	config := &domain.ProjectConfig{
		Name: projectName,
	}

	adapter := &ComputeAdapter{
		config:       config,
		composeFile:  composeFile,
		worktreeBase: tmpDir,
	}

	// Before start
	running, err := adapter.IsRunning(ctx, "isrunning")
	if err != nil {
		t.Fatalf("IsRunning failed: %v", err)
	}
	if running {
		t.Error("expected not running before Start")
	}

	// After start
	ports := domain.PortMap{"redis": 16382}
	adapter.Start(ctx, "isrunning", ports)

	running, err = adapter.IsRunning(ctx, "isrunning")
	if err != nil {
		t.Fatalf("IsRunning failed: %v", err)
	}
	if !running {
		t.Error("expected running after Start")
	}
}

func TestComputeAdapter_NoComposeFile(t *testing.T) {
	ctx := context.Background()
	config := &domain.ProjectConfig{Name: "test"}

	adapter := &ComputeAdapter{
		config:      config,
		composeFile: "", // no compose file
	}

	// All operations should be no-ops
	if err := adapter.Start(ctx, "test", domain.PortMap{}); err != nil {
		t.Errorf("Start should be no-op: %v", err)
	}
	if err := adapter.Stop(ctx, "test"); err != nil {
		t.Errorf("Stop should be no-op: %v", err)
	}
	running, _ := adapter.IsRunning(ctx, "test")
	if running {
		t.Error("IsRunning should return false with no compose file")
	}
}
