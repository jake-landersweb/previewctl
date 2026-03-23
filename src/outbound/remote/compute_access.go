package remote

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
)

// SSHComputeAccess implements domain.ComputeAccess over SSH.
// It uses the system ssh and scp binaries.
type SSHComputeAccess struct {
	host string
	user string
	root string // remote working directory, e.g., "/app"
}

// NewSSHComputeAccess creates a ComputeAccess backed by SSH to a remote host.
func NewSSHComputeAccess(host, user, root string) *SSHComputeAccess {
	return &SSHComputeAccess{host: host, user: user, root: root}
}

func (s *SSHComputeAccess) WriteFile(ctx context.Context, relPath string, data []byte, _ os.FileMode) error {
	remotePath := path.Join(s.root, relPath)

	// Ensure parent directory exists
	mkdirCmd := fmt.Sprintf("mkdir -p %q", path.Dir(remotePath))
	if err := s.sshRun(ctx, mkdirCmd, nil); err != nil {
		return fmt.Errorf("creating remote directory: %w", err)
	}

	// Pipe content through ssh
	cmd := exec.CommandContext(ctx, "ssh", s.target(), fmt.Sprintf("cat > %q", remotePath))
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing remote file %s: %w", relPath, err)
	}
	return nil
}

func (s *SSHComputeAccess) ReadFile(ctx context.Context, relPath string) ([]byte, error) {
	remotePath := path.Join(s.root, relPath)
	cmd := exec.CommandContext(ctx, "ssh", s.target(), fmt.Sprintf("cat %q", remotePath))
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("reading remote file %s: %w", relPath, err)
	}
	return out, nil
}

func (s *SSHComputeAccess) Exec(ctx context.Context, command string, env []string) (string, error) {
	// Build env prefix for the remote command
	envPrefix := ""
	for _, e := range env {
		envPrefix += fmt.Sprintf("export %s; ", e)
	}

	remoteCmd := fmt.Sprintf("cd %q && %s%s", s.root, envPrefix, command)
	cmd := exec.CommandContext(ctx, "ssh", s.target(), remoteCmd)
	cmd.Stderr = os.Stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("remote exec: %w", err)
	}
	return stdout.String(), nil
}

func (s *SSHComputeAccess) Root() string {
	return s.root
}

func (s *SSHComputeAccess) target() string {
	return fmt.Sprintf("%s@%s", s.user, s.host)
}

func (s *SSHComputeAccess) sshRun(ctx context.Context, command string, env []string) error {
	_, err := s.Exec(ctx, command, env)
	return err
}
