package domain

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
)

// DomainSSHComputeAccess implements ComputeAccess over SSH.
// It uses the system ssh binary. Lives in domain to avoid circular imports.
type DomainSSHComputeAccess struct {
	host string
	user string
	root string // remote working directory, e.g., "/app"
}

// NewDomainSSHComputeAccess creates a ComputeAccess backed by SSH to a remote host.
func NewDomainSSHComputeAccess(host, user, root string) ComputeAccess {
	return &DomainSSHComputeAccess{host: host, user: user, root: root}
}

// Host returns the SSH host.
func (s *DomainSSHComputeAccess) Host() string { return s.host }

// User returns the SSH user.
func (s *DomainSSHComputeAccess) User() string { return s.user }

func (s *DomainSSHComputeAccess) WriteFile(ctx context.Context, relPath string, data []byte, _ os.FileMode) error {
	remotePath := path.Join(s.root, relPath)

	// Ensure parent directory exists
	mkdirCmd := fmt.Sprintf("mkdir -p %q", path.Dir(remotePath))
	cmd := exec.CommandContext(ctx, "ssh", s.target(), mkdirCmd)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating remote directory: %w", err)
	}

	// Pipe content through ssh
	cmd = exec.CommandContext(ctx, "ssh", s.target(), fmt.Sprintf("cat > %q", remotePath))
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing remote file %s: %w", relPath, err)
	}
	return nil
}

func (s *DomainSSHComputeAccess) ReadFile(ctx context.Context, relPath string) ([]byte, error) {
	remotePath := path.Join(s.root, relPath)
	cmd := exec.CommandContext(ctx, "ssh", s.target(), fmt.Sprintf("cat %q", remotePath))
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("reading remote file %s: %w", relPath, err)
	}
	return out, nil
}

func (s *DomainSSHComputeAccess) Exec(ctx context.Context, command string, env []string) (string, error) {
	// Build env prefix for the remote command
	var envPrefix strings.Builder
	for _, e := range env {
		fmt.Fprintf(&envPrefix, "export %s; ", e)
	}

	remoteCmd := fmt.Sprintf("cd %q && %s%s", s.root, envPrefix.String(), command)
	cmd := exec.CommandContext(ctx, "ssh", s.target(), remoteCmd)
	cmd.Stderr = os.Stderr

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("remote exec: %w", err)
	}
	return stdout.String(), nil
}

func (s *DomainSSHComputeAccess) Root() string {
	return s.root
}

func (s *DomainSSHComputeAccess) target() string {
	return fmt.Sprintf("%s@%s", s.user, s.host)
}
