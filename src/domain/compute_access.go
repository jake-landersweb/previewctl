package domain

import (
	"context"
	"os"
)

// ComputeAccess provides uniform access to a compute location.
// Local mode wraps filesystem operations; remote mode wraps SSH/SCP.
type ComputeAccess interface {
	// WriteFile writes content to a path relative to the compute root.
	WriteFile(ctx context.Context, relPath string, data []byte, mode os.FileMode) error

	// ReadFile reads content from a path relative to the compute root.
	ReadFile(ctx context.Context, relPath string) ([]byte, error)

	// Exec runs a command in the compute root directory.
	// Stderr streams to the configured writer. Stdout is captured and returned silently.
	Exec(ctx context.Context, command string, env []string) (stdout string, err error)

	// VerboseExec runs a command and tees stdout to stderr for real-time visibility.
	// Use for user-defined hooks and build commands where output should always be visible.
	VerboseExec(ctx context.Context, command string, env []string) (stdout string, err error)

	// Root returns the compute root path (local path or remote working dir).
	Root() string
}
