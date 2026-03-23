package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jake-landersweb/previewctl/src/domain"
	filestate "github.com/jake-landersweb/previewctl/src/outbound/state"
	"github.com/spf13/cobra"
)

// orphanedResource describes a resource that will be cleaned up.
type orphanedResource struct {
	kind string // "worktree", "compose"
	name string // display name / identifier
}

func newCleanCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Find and remove dangling resources (worktrees, containers)",
		Long: `Scans for orphaned resources across all projects managed by previewctl:
  - Git worktrees under ~/.previewctl/worktrees not tracked in state
  - Docker compose projects that are still running for deleted environments

Resources are shown for review before any deletion occurs.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			Header("Scanning for dangling resources")

			// --- Phase 1: Scan ---

			home, _ := os.UserHomeDir()
			cacheDir := filepath.Join(home, ".cache", "previewctl")

			spinner := newLiveSpinner("Loading project state...")
			spinner.Start()

			projects, err := discoverProjects(cacheDir)
			if err != nil {
				spinner.Stop()
				KeyValue("State", fmt.Sprintf("no state directory found (%s)", cacheDir))
			}

			trackedWorktrees := make(map[string]bool)
			trackedComposeProjects := make(map[string]bool)
			knownProjectNames := make([]string, 0, len(projects))
			knownProjectNames = append(knownProjectNames, projects...)

			for _, proj := range projects {
				statePath := filepath.Join(cacheDir, proj, "state.json")
				adapter := filestate.NewFileStateAdapter(statePath)
				state, err := adapter.Load(context.TODO())
				if err != nil {
					continue
				}
				for _, entry := range state.Environments {
					if wt := entry.WorktreePath(); wt != "" {
						trackedWorktrees[wt] = true
					}
					composeName := domain.ComposeProjectName(proj, entry.Name)
					trackedComposeProjects[composeName] = true
				}
			}
			spinner.Stop()
			fmt.Fprintf(os.Stderr, "  %s %s\n\n",
				styleSuccess.Render("✓"),
				styleDim.Render(fmt.Sprintf("Loaded state for %d project(s)", len(projects))),
			)

			var orphaned []orphanedResource

			// Scan worktrees
			SectionHeader("Git worktrees")
			spinner = newLiveSpinner("Scanning worktrees...")
			spinner.Start()
			orphanedWorktrees := findOrphanedWorktrees(trackedWorktrees)
			spinner.Stop()
			if len(orphanedWorktrees) == 0 {
				fmt.Fprintf(os.Stderr, "  %s %s\n", styleSuccess.Render("✓"), styleDim.Render("No orphaned worktrees"))
			} else {
				for _, wt := range orphanedWorktrees {
					fmt.Fprintf(os.Stderr, "  %s %s\n", styleSkipped.Render("⚠"), styleMessage.Render(wt))
					orphaned = append(orphaned, orphanedResource{kind: "worktree", name: wt})
				}
			}
			fmt.Fprintln(os.Stderr)

			// Scan compose projects
			SectionHeader("Docker compose projects")
			spinner = newLiveSpinner("Scanning compose projects...")
			spinner.Start()
			orphanedCompose := findOrphanedComposeProjects(trackedComposeProjects, knownProjectNames)
			spinner.Stop()
			if len(orphanedCompose) == 0 {
				fmt.Fprintf(os.Stderr, "  %s %s\n", styleSuccess.Render("✓"), styleDim.Render("No orphaned compose projects"))
			} else {
				for _, proj := range orphanedCompose {
					fmt.Fprintf(os.Stderr, "  %s %s\n", styleSkipped.Render("⚠"), styleMessage.Render(proj))
					orphaned = append(orphaned, orphanedResource{kind: "compose", name: proj})
				}
			}

			fmt.Fprintln(os.Stderr)

			// --- Phase 2: Confirm & Delete ---

			if len(orphaned) == 0 {
				Success("Everything is clean")
				return nil
			}

			if dryRun {
				fmt.Fprintf(os.Stderr, "%s %s\n\n",
					styleSkipped.Render("⚠"),
					styleSkipped.Render(fmt.Sprintf("Found %d orphaned resource(s). Run without --dry-run to clean up.", len(orphaned))),
				)
				return nil
			}

			// Ask for confirmation
			fmt.Fprintf(os.Stderr, "%s %s\n",
				styleSkipped.Render("⚠"),
				styleSkipped.Render(fmt.Sprintf("Found %d orphaned resource(s) to remove.", len(orphaned))),
			)
			fmt.Fprintf(os.Stderr, "\n  %s ", styleMessage.Render("Proceed with cleanup? [y/N]"))

			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))

			if answer != "y" && answer != "yes" {
				fmt.Fprintf(os.Stderr, "\n  %s\n\n", styleDim.Render("Cleanup cancelled."))
				return nil
			}

			fmt.Fprintln(os.Stderr)
			cleaned := 0
			for _, r := range orphaned {
				s := newLiveSpinner(fmt.Sprintf("Removing %s...", r.name))
				s.Start()
				switch r.kind {
				case "worktree":
					removeWorktree(r.name)
				case "compose":
					removeComposeProject(r.name)
				}
				s.Stop()
				cleaned++
			}

			fmt.Fprintln(os.Stderr)
			Success(fmt.Sprintf("Cleaned up %d resource(s)", cleaned))

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be cleaned without actually removing anything")

	return cmd
}

// discoverProjects lists project directories under the cache dir.
func discoverProjects(cacheDir string) ([]string, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil, err
	}
	var projects []string
	for _, e := range entries {
		if e.IsDir() {
			projects = append(projects, e.Name())
		}
	}
	return projects, nil
}

// findOrphanedWorktrees scans ~/.previewctl/worktrees for directories
// that aren't tracked in any previewctl state.
func findOrphanedWorktrees(tracked map[string]bool) []string {
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".previewctl", "worktrees")

	// Walk project dirs under the base path
	projectDirs, err := os.ReadDir(base)
	if err != nil {
		return nil
	}

	var orphaned []string
	for _, projDir := range projectDirs {
		if !projDir.IsDir() {
			continue
		}
		projPath := filepath.Join(base, projDir.Name())
		envDirs, err := os.ReadDir(projPath)
		if err != nil {
			continue
		}
		for _, envDir := range envDirs {
			if !envDir.IsDir() {
				continue
			}
			wtPath := filepath.Join(projPath, envDir.Name())
			if !tracked[wtPath] {
				orphaned = append(orphaned, wtPath)
			}
		}
	}

	return orphaned
}

// findOrphanedComposeProjects uses `docker compose ls` to find
// previewctl-managed projects that aren't tracked in state.
func findOrphanedComposeProjects(tracked map[string]bool, knownProjectNames []string) []string {
	cmd := exec.Command("docker", "compose", "ls", "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var projects []struct {
		Name string `json:"Name"`
	}
	if err := json.Unmarshal(out, &projects); err != nil {
		return nil
	}

	var orphaned []string
	for _, p := range projects {
		if tracked[p.Name] {
			continue
		}
		// Only flag projects whose name starts with a known previewctl project name
		for _, prefix := range knownProjectNames {
			if strings.HasPrefix(p.Name, prefix+"-") {
				orphaned = append(orphaned, p.Name)
				break
			}
		}
	}

	return orphaned
}

func removeWorktree(path string) {
	cmd := exec.Command("git", "worktree", "remove", path, "--force")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "  %s %s: %s\n", styleFail.Render("✗"), styleFail.Render(path), strings.TrimSpace(string(out)))
	} else {
		fmt.Fprintf(os.Stderr, "  %s %s\n", styleSuccess.Render("✓"), styleMessage.Render(fmt.Sprintf("Removed worktree %s", path)))
	}
}

func removeComposeProject(name string) {
	cmd := exec.Command("docker", "compose", "-p", name, "down", "-v")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "  %s %s: %s\n", styleFail.Render("✗"), styleFail.Render(name), strings.TrimSpace(string(out)))
	} else {
		_ = out
		fmt.Fprintf(os.Stderr, "  %s %s\n", styleSuccess.Render("✓"), styleMessage.Render(fmt.Sprintf("Removed compose project %s", name)))
	}
}
