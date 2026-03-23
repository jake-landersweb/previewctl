package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
)

func newAttachCmd() *cobra.Command {
	var worktreePath string

	cmd := &cobra.Command{
		Use:   "attach [name]",
		Short: "Create a preview environment using an existing worktree",
		Long: `Attach sets up a preview environment (ports, core services, env files,
infrastructure) for a worktree that already exists — e.g., one created by
Claude Code, GitHub Codex, or manually via git worktree add.

The worktree itself is not managed by previewctl and will not be removed on delete.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve worktree path
			if worktreePath == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting cwd: %w", err)
				}
				worktreePath = cwd
			}
			absPath, err := filepath.Abs(worktreePath)
			if err != nil {
				return fmt.Errorf("resolving path: %w", err)
			}

			// Verify directory exists
			info, err := os.Stat(absPath)
			if err != nil || !info.IsDir() {
				return fmt.Errorf("worktree path does not exist: %s", absPath)
			}

			// Verify it's a git repo
			if err := exec.Command("git", "-C", absPath, "rev-parse", "--git-dir").Run(); err != nil {
				return fmt.Errorf("%s is not a git repository", absPath)
			}

			// Resolve name
			envName := ""
			if len(args) > 0 {
				envName = args[0]
			} else {
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
			KeyValue("Managed", "false (external worktree)")

			SectionHeader("Ports")
			portNames := make([]string, 0, len(entry.Ports))
			for name := range entry.Ports {
				portNames = append(portNames, name)
			}
			sort.Strings(portNames)
			for _, name := range portNames {
				DetailKeyValue(name, fmt.Sprintf("%d", entry.Ports[name]))
			}
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVarP(&worktreePath, "worktree", "w", "", "Path to existing worktree (defaults to current directory)")

	return cmd
}
