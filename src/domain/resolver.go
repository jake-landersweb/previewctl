package domain

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ResolveEnvironmentFromCwd determines the environment name by checking if the
// current working directory is inside any known worktree path from state.
func ResolveEnvironmentFromCwd(cwd string, environments map[string]*EnvironmentEntry) (string, error) {
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolving cwd: %w", err)
	}

	for name, entry := range environments {
		wtPath := entry.WorktreePath()
		if wtPath == "" {
			continue
		}
		absWorktree, err := filepath.Abs(wtPath)
		if err != nil {
			continue
		}
		// Check if cwd is the worktree path or a subdirectory of it
		if absCwd == absWorktree || strings.HasPrefix(absCwd, absWorktree+string(filepath.Separator)) {
			return name, nil
		}
	}

	return "", fmt.Errorf("not inside any known environment worktree")
}
