package local

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jake-landersweb/previewctl/src/domain"
)

// ComputeAdapter implements domain.ComputePort for local development.
// It manages git worktrees and per-environment docker compose infrastructure.
type ComputeAdapter struct {
	config       *domain.ProjectConfig
	composeFile  string // path to per-env compose file (e.g., compose.worktree.yaml)
	worktreeBase string // base directory for worktrees
}

// NewComputeAdapter creates a new local compute adapter.
func NewComputeAdapter(config *domain.ProjectConfig, composeFile string) *ComputeAdapter {
	base := "~/worktrees"
	if config.Local != nil && config.Local.Worktree.BasePath != "" {
		base = config.Local.Worktree.BasePath
	}
	// Expand ~ to home directory
	if strings.HasPrefix(base, "~/") {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, base[2:])
	}

	return &ComputeAdapter{
		config:       config,
		composeFile:  composeFile,
		worktreeBase: base,
	}
}

func (a *ComputeAdapter) Create(ctx context.Context, envName string, branch string) (*domain.ComputeResources, error) {
	worktreePath := filepath.Join(a.worktreeBase, a.config.Name, envName)

	// Create worktree
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", worktreePath, "-b", branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		// Try without -b if branch already exists
		cmd = exec.CommandContext(ctx, "git", "worktree", "add", worktreePath, branch)
		if out2, err2 := cmd.CombinedOutput(); err2 != nil {
			return nil, fmt.Errorf("creating worktree: %s\n%s", string(out), string(out2))
		}
		_ = out
	}

	// Update submodules if they exist
	cmd = exec.CommandContext(ctx, "git", "submodule", "update", "--init", "--recursive")
	cmd.Dir = worktreePath
	_, _ = cmd.CombinedOutput() // ignore errors if no submodules

	// Install dependencies if package manager is configured
	if a.config.PackageManager != "" {
		cmd = exec.CommandContext(ctx, a.config.PackageManager, "install")
		cmd.Dir = worktreePath
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("installing dependencies: %s", string(out))
		}
	}

	return &domain.ComputeResources{
		WorktreePath: worktreePath,
	}, nil
}

func (a *ComputeAdapter) Start(ctx context.Context, envName string, ports domain.PortMap) error {
	if a.composeFile == "" {
		return nil
	}

	env := a.buildComposeEnv(envName, ports)
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", a.composeFile, "up", "-d")
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("starting infrastructure: %s", string(out))
	}

	return nil
}

func (a *ComputeAdapter) Stop(ctx context.Context, envName string) error {
	if a.composeFile == "" {
		return nil
	}

	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", a.composeFile, "stop")
	cmd.Env = a.composeEnv(envName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("stopping infrastructure: %s", string(out))
	}

	return nil
}

func (a *ComputeAdapter) Destroy(ctx context.Context, envName string) error {
	// Tear down docker compose
	if a.composeFile != "" {
		cmd := exec.CommandContext(ctx, "docker", "compose", "-f", a.composeFile, "down", "-v")
		cmd.Env = a.composeEnv(envName)
		if out, err := cmd.CombinedOutput(); err != nil {
			// Log but don't fail — worktree removal should still happen
			_ = out
		}
	}

	// Remove worktree
	worktreePath := filepath.Join(a.worktreeBase, a.config.Name, envName)
	cmd := exec.CommandContext(ctx, "git", "worktree", "remove", worktreePath, "--force")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("removing worktree: %s", string(out))
	}

	return nil
}

func (a *ComputeAdapter) IsRunning(ctx context.Context, envName string) (bool, error) {
	if a.composeFile == "" {
		return false, nil
	}

	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", a.composeFile, "ps", "--format", "json")
	cmd.Env = a.composeEnv(envName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, nil
	}

	// If output is non-empty, containers are running
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// composeEnv returns os.Environ() with COMPOSE_PROJECT_NAME set for the given environment.
func (a *ComputeAdapter) composeEnv(envName string) []string {
	return append(os.Environ(), fmt.Sprintf("COMPOSE_PROJECT_NAME=%s-%s", a.config.Name, envName))
}

func (a *ComputeAdapter) buildComposeEnv(envName string, ports domain.PortMap) []string {
	env := []string{
		fmt.Sprintf("COMPOSE_PROJECT_NAME=%s-%s", a.config.Name, envName),
	}

	// Pass all infrastructure ports as env vars (e.g., REDIS_PORT=6421)
	for name, port := range ports {
		envVar := fmt.Sprintf("%s_PORT=%d", strings.ToUpper(strings.ReplaceAll(name, "-", "_")), port)
		env = append(env, envVar)
	}

	return env
}
