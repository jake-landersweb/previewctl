package domain

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
)

// DomainSSHComputeAccess implements ComputeAccess over SSH.
// It uses the system ssh binary. Lives in domain to avoid circular imports.
//
// Two connection modes:
//   - ProxyCommand mode: uses -o ProxyCommand=... (cloud-agnostic, no local SSH config needed)
//   - Direct mode: uses user@host (requires SSH config or direct access)
type DomainSSHComputeAccess struct {
	host         string // SSH host alias or IP (direct mode)
	user         string
	root         string // remote working directory, e.g., "/app"
	proxyCommand string // SSH ProxyCommand (proxy mode). When set, host is ignored for routing.
	stderr       io.Writer
}

// SSHComputeAccessOpts configures SSH compute access creation.
type SSHComputeAccessOpts struct {
	Host         string // SSH host (direct mode) or logical hostname (proxy mode)
	User         string
	Root         string
	ProxyCommand string // when set, uses -o ProxyCommand=... instead of relying on SSH config
}

// NewDomainSSHComputeAccess creates a ComputeAccess backed by SSH to a remote host.
// This is the direct-mode constructor (backward compatible).
func NewDomainSSHComputeAccess(host, user, root string) ComputeAccess {
	return &DomainSSHComputeAccess{host: host, user: user, root: root, stderr: os.Stderr}
}

// NewDomainSSHComputeAccessWithOpts creates a ComputeAccess with full SSH options.
func NewDomainSSHComputeAccessWithOpts(opts SSHComputeAccessOpts) ComputeAccess {
	return &DomainSSHComputeAccess{
		host:         opts.Host,
		user:         opts.User,
		root:         opts.Root,
		proxyCommand: opts.ProxyCommand,
		stderr:       os.Stderr,
	}
}

func (s *DomainSSHComputeAccess) SetStderr(w io.Writer) { s.stderr = w }

// Host returns the SSH host.
func (s *DomainSSHComputeAccess) Host() string { return s.host }

// User returns the SSH user.
func (s *DomainSSHComputeAccess) User() string { return s.user }

// ProxyCommand returns the configured proxy command, if any.
func (s *DomainSSHComputeAccess) ProxyCommand() string { return s.proxyCommand }

func (s *DomainSSHComputeAccess) WriteFile(ctx context.Context, relPath string, data []byte, _ os.FileMode) error {
	remotePath := path.Join(s.root, relPath)

	// Ensure parent directory exists
	mkdirCmd := fmt.Sprintf("mkdir -p %q", path.Dir(remotePath))
	cmd := s.sshCmd(ctx, mkdirCmd)
	cmd.Stderr = s.stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating remote directory: %w", err)
	}

	// Pipe content through ssh
	cmd = s.sshCmd(ctx, fmt.Sprintf("cat > %q", remotePath))
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stderr = s.stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing remote file %s: %w", relPath, err)
	}
	return nil
}

func (s *DomainSSHComputeAccess) ReadFile(ctx context.Context, relPath string) ([]byte, error) {
	remotePath := path.Join(s.root, relPath)
	cmd := s.sshCmd(ctx, fmt.Sprintf("cat %q", remotePath))
	cmd.Stderr = s.stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("reading remote file %s: %w", relPath, err)
	}
	return out, nil
}

func (s *DomainSSHComputeAccess) Exec(ctx context.Context, command string, env []string) (string, error) {
	// Only export previewctl-relevant vars to the remote — skip the local OS
	// environment which may contain paths/values that break the remote shell.
	var envPrefix strings.Builder
	for _, e := range env {
		if strings.HasPrefix(e, "PREVIEWCTL_") ||
			strings.HasPrefix(e, "COMPOSE_") ||
			strings.HasSuffix(strings.SplitN(e, "=", 2)[0], "_PORT") {
			fmt.Fprintf(&envPrefix, "export %s; ", e)
		}
	}

	remoteCmd := fmt.Sprintf("set -a; [ -f /etc/environment ] && . /etc/environment; set +a; cd %q && %s%s", s.root, envPrefix.String(), command)
	cmd := s.sshCmd(ctx, remoteCmd)
	fmt.Fprintf(s.stderr, "[ssh-debug] exec: %s %s\n", cmd.Path, strings.Join(cmd.Args[1:], " "))
	cmd.Stderr = s.stderr

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

// sshCmd builds an *exec.Cmd for SSH with the appropriate connection arguments.
// In proxy mode, it uses -o ProxyCommand=... to tunnel through a cloud provider.
// In direct mode, it uses user@host.
func (s *DomainSSHComputeAccess) sshCmd(ctx context.Context, remoteCmd string) *exec.Cmd {
	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
	}

	if s.proxyCommand != "" {
		args = append(args, "-o", fmt.Sprintf("ProxyCommand=%s", s.proxyCommand))
		// With ProxyCommand, we still need a target. Use the host as a logical name
		// (the actual routing is done by the proxy command).
		args = append(args, s.target())
	} else {
		args = append(args, s.target())
	}

	args = append(args, remoteCmd)
	return exec.CommandContext(ctx, "ssh", args...)
}

// sshArgs returns the SSH arguments for building external SSH commands (e.g., for syscall.Exec).
func (s *DomainSSHComputeAccess) SSHArgs() []string {
	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
	}
	if s.proxyCommand != "" {
		args = append(args, "-o", fmt.Sprintf("ProxyCommand=%s", s.proxyCommand))
	}
	args = append(args, s.target())
	return args
}

func (s *DomainSSHComputeAccess) target() string {
	return fmt.Sprintf("%s@%s", s.user, s.host)
}
