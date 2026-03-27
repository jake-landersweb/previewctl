package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newCreateCmd() *cobra.Command {
	var (
		branch       string
		noCache      bool
		worktreePath string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new development environment",
		Long: `Create provisions and starts a new environment. By default, previewctl
creates and manages its own git worktree.

Use --worktree to attach to an existing worktree instead (e.g., one
created by Claude Code, GitHub Codex, or manually via git worktree add).
The worktree will not be removed on delete.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Attach mode: use existing worktree
			if worktreePath != "" {
				return runAttach(cmd, worktreePath)
			}

			envName := globalEnvName
			if envName == "" {
				return fmt.Errorf("--env (-e) is required for create")
			}
			if branch == "" {
				branch = envName
			}

			progress := NewCLIProgressReporter()
			mgr, cfg, err := buildManager(progress)
			if err != nil {
				return err
			}
			if noCache {
				mgr.SetNoCache(true)
			}

			Header(fmt.Sprintf("Creating environment %s",
				styleDetail.Render(envName)))

			entry, err := mgr.Init(cmd.Context(), envName, branch)
			if err != nil {
				return err
			}

			Success(fmt.Sprintf("Environment %s is ready",
				styleDetail.Render(entry.Name)))

			KeyValue("Branch", entry.Branch)
			if wt := entry.WorktreePath(); wt != "" {
				KeyValue("Worktree", wt)
			}

			var domain string
			if cfg.Runner != nil && cfg.Runner.Compose != nil && cfg.Runner.Compose.Proxy != nil {
				domain = cfg.Runner.Compose.Proxy.Domain
			}
			PrintServiceURLs(envName, entry.Ports, domain)
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Git branch name (defaults to environment name)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Skip all step caching, re-run everything")
	cmd.Flags().StringVarP(&worktreePath, "worktree", "w", "", "Attach to an existing worktree instead of creating one")

	return cmd
}

// runAttach handles the --worktree flag on create.
func runAttach(cmd *cobra.Command, worktreePath string) error {
	absPath, err := filepath.Abs(worktreePath)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("worktree path does not exist: %s", absPath)
	}

	if err := exec.Command("git", "-C", absPath, "rev-parse", "--git-dir").Run(); err != nil {
		return fmt.Errorf("%s is not a git repository", absPath)
	}

	envName := globalEnvName
	if envName == "" {
		envName = filepath.Base(absPath)
	}

	progress := NewCLIProgressReporter()
	mgr, _, err := buildManager(progress)
	if err != nil {
		return err
	}

	Header(fmt.Sprintf("Attaching environment %s",
		styleDetail.Render(envName)))
	KeyValue("Worktree", absPath)
	fmt.Fprintln(os.Stderr)

	entry, err := mgr.Attach(cmd.Context(), envName, absPath)
	if err != nil {
		return err
	}

	Success(fmt.Sprintf("Environment %s is ready",
		styleDetail.Render(entry.Name)))

	KeyValue("Branch", entry.Branch)
	KeyValue("Worktree", entry.WorktreePath())
	PrintServiceURLs(envName, entry.Ports, "")
	fmt.Println()

	return nil
}
