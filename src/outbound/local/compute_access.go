package local

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// LocalComputeAccess implements domain.ComputeAccess for local filesystem operations.
type LocalComputeAccess struct {
	root   string
	stderr io.Writer
}

// NewLocalComputeAccess creates a ComputeAccess backed by a local filesystem path.
func NewLocalComputeAccess(root string) *LocalComputeAccess {
	return &LocalComputeAccess{root: root, stderr: os.Stderr}
}

func (l *LocalComputeAccess) SetStderr(w io.Writer) { l.stderr = w }

func (l *LocalComputeAccess) WriteFile(_ context.Context, relPath string, data []byte, mode os.FileMode) error {
	absPath := filepath.Join(l.root, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return fmt.Errorf("creating directory for %s: %w", relPath, err)
	}
	return os.WriteFile(absPath, data, mode)
}

func (l *LocalComputeAccess) ReadFile(_ context.Context, relPath string) ([]byte, error) {
	return os.ReadFile(filepath.Join(l.root, relPath))
}

func (l *LocalComputeAccess) Exec(ctx context.Context, command string, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = l.root
	cmd.Env = append(os.Environ(), env...)
	cmd.Stderr = l.stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("exec in %s: %w", l.root, err)
	}
	return stdout.String(), nil
}

func (l *LocalComputeAccess) Root() string {
	return l.root
}
