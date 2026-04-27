package domain

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
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
	identityFile string // path to SSH private key (optional)
	stderr       io.Writer
}

// SSHComputeAccessOpts configures SSH compute access creation.
type SSHComputeAccessOpts struct {
	Host         string // SSH host (direct mode) or logical hostname (proxy mode)
	User         string
	Root         string
	ProxyCommand string // when set, uses -o ProxyCommand=... instead of relying on SSH config
	IdentityFile string // path to SSH private key (optional)
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
		identityFile: opts.IdentityFile,
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

// EnsureRootWritable reassigns ownership of the remote root tree to the
// current SSH user and relaxes its mode so subsequent host-side tools (pnpm,
// turbo, etc.) can create and chmod files regardless of who touched the tree
// last.
//
// This exists because OS Login hands each actor a distinct UID; files
// created by one actor (CI service account, another human) are owned by
// that UID, and chmod(2) on those inodes fails for anyone but the owner
// even when the mode is world-writable. Call this at the top of any
// operation that writes into the remote root from a potentially different
// actor than the last writer.
//
// Idempotent and best-effort: failures are swallowed (`|| true`) because the
// common case is already-correctly-owned, and we don't want to block the
// happy path on an unrelated permission blip.
func (s *DomainSSHComputeAccess) EnsureRootWritable(ctx context.Context) error {
	script := fmt.Sprintf(
		`sudo chown -R "$(whoami)":"$(id -gn)" %q 2>/dev/null || true; sudo chmod -R a+rwX %q 2>/dev/null || true`,
		s.root, s.root,
	)
	_, err := s.Exec(ctx, script, nil)
	return err
}

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
	return s.execInternal(ctx, command, env, false)
}

func (s *DomainSSHComputeAccess) VerboseExec(ctx context.Context, command string, env []string) (string, error) {
	return s.execInternal(ctx, command, env, true)
}

func (s *DomainSSHComputeAccess) execInternal(ctx context.Context, command string, env []string, teeStdout bool) (string, error) {
	// Write environment variables to a temp file on the remote host, then source
	// it before running the command. This avoids SSH command-line length limits
	// that can silently truncate long inline export chains.
	//
	// Path is under $HOME (not /tmp) so different OS Login users on the same
	// VM — e.g., a CI service account and a human developer — each own their
	// own copy. A shared /tmp path caused EACCES whenever the second user
	// tried to overwrite the first user's file.
	const remoteEnvFile = "$HOME/.previewctl-env.sh"

	var envContent strings.Builder
	for _, e := range env {
		if strings.HasPrefix(e, "PREVIEWCTL_") ||
			strings.HasPrefix(e, "COMPOSE_") ||
			strings.HasSuffix(strings.SplitN(e, "=", 2)[0], "_PORT") {
			fmt.Fprintf(&envContent, "export %s\n", e)
		}
	}

	if envContent.Len() > 0 {
		writeCmd := s.sshCmd(ctx, fmt.Sprintf("cat > %s", remoteEnvFile))
		writeCmd.Stdin = strings.NewReader(envContent.String())
		writeCmd.Stderr = s.stderr
		if err := writeCmd.Run(); err != nil {
			return "", fmt.Errorf("writing remote env file: %w", err)
		}
	}

	remoteCmd := fmt.Sprintf(
		"set -a; [ -f /etc/environment ] && . /etc/environment; [ -f %s ] && . %s; set +a; cd %q && %s",
		remoteEnvFile, remoteEnvFile, s.root, command,
	)
	cmd := s.sshCmd(ctx, remoteCmd)
	cmd.Stderr = s.stderr

	var stdout bytes.Buffer
	if teeStdout {
		cmd.Stdout = io.MultiWriter(&stdout, s.stderr)
	} else {
		cmd.Stdout = &stdout
	}

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

	if s.identityFile != "" {
		expanded := s.identityFile
		if strings.HasPrefix(expanded, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				expanded = filepath.Join(home, expanded[2:])
			}
		}
		args = append(args, "-i", expanded)
	}

	if s.proxyCommand != "" {
		args = append(args, "-o", fmt.Sprintf("ProxyCommand=%s", s.proxyCommand))
	}
	args = append(args, s.target())

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
	if s.identityFile != "" {
		expanded := s.identityFile
		if strings.HasPrefix(expanded, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				expanded = filepath.Join(home, expanded[2:])
			}
		}
		args = append(args, "-i", expanded)
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
