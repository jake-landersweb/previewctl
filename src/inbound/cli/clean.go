package cli

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	_ "github.com/lib/pq"

	"github.com/jake-landersweb/previewctl/src/domain"
	filestate "github.com/jake-landersweb/previewctl/src/outbound/state"
	"github.com/spf13/cobra"
)

// orphanedResource describes a resource that will be cleaned up.
type orphanedResource struct {
	kind  string // "worktree", "compose", "database"
	name  string // display name / identifier
	dbCfg *domain.DatabaseModeConfig // only set for database resources
}

func newCleanCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Find and remove dangling resources (worktrees, containers, databases)",
		Long: `Scans for orphaned resources across all projects managed by previewctl:
  - Git worktrees under ~/.previewctl/worktrees not tracked in state
  - Docker compose projects that are still running for deleted environments
  - Databases cloned from templates that are no longer tracked

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
			trackedDatabases := make(map[string]bool)
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
					if entry.Local != nil {
						if entry.Local.WorktreePath != "" {
							trackedWorktrees[entry.Local.WorktreePath] = true
						}
						if entry.Local.ComposeProjectName != "" {
							trackedComposeProjects[entry.Local.ComposeProjectName] = true
						}
					}
					for _, dbRef := range entry.Databases {
						trackedDatabases[dbRef.Name] = true
					}
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

			// Scan databases
			SectionHeader("Databases")
			cfg, _, configErr := loadConfig()
			if configErr != nil {
				fmt.Fprintf(os.Stderr, "  %s %s\n", styleDim.Render("−"), styleDim.Render("No project config found, skipping database scan"))
			} else {
				for dbName, dbCfg := range cfg.Core.Databases {
					if dbCfg.Engine != "postgres" || dbCfg.Local == nil {
						continue
					}
					spinner = newLiveSpinner(fmt.Sprintf("Scanning %s databases...", dbName))
					spinner.Start()
					orphanedDbs := findOrphanedDatabases(*dbCfg.Local, trackedDatabases)
					spinner.Stop()
					if len(orphanedDbs) == 0 {
						fmt.Fprintf(os.Stderr, "  %s %s\n",
							styleSuccess.Render("✓"),
							styleDim.Render(fmt.Sprintf("No orphaned databases for %s", dbName)),
						)
					} else {
						for _, db := range orphanedDbs {
							fmt.Fprintf(os.Stderr, "  %s %s\n", styleSkipped.Render("⚠"), styleMessage.Render(db))
							modeCfgCopy := *dbCfg.Local
							orphaned = append(orphaned, orphanedResource{kind: "database", name: db, dbCfg: &modeCfgCopy})
						}
					}
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
				case "database":
					if r.dbCfg != nil {
					dropDatabase(*r.dbCfg, r.name)
				}
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
	base := domain.WorktreeBasePath()

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

// findOrphanedDatabases queries Postgres for databases matching the wt_ prefix
// that aren't tracked in state.
func findOrphanedDatabases(dbCfg domain.DatabaseModeConfig, tracked map[string]bool) []string {
	dsn := fmt.Sprintf("host=localhost port=%d user=%s password=%s dbname=postgres sslmode=disable",
		dbCfg.Port, dbCfg.User, dbCfg.Password)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil
	}
	defer func() { _ = db.Close() }()

	rows, err := db.Query("SELECT datname FROM pg_database WHERE datname LIKE 'wt_%' AND datistemplate = false")
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var orphaned []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		if !tracked[name] {
			orphaned = append(orphaned, name)
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

func dropDatabase(dbCfg domain.DatabaseModeConfig, dbName string) {
	dsn := fmt.Sprintf("host=localhost port=%d user=%s password=%s dbname=postgres sslmode=disable",
		dbCfg.Port, dbCfg.User, dbCfg.Password)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %s %s: %v\n", styleFail.Render("✗"), styleFail.Render(dbName), err)
		return
	}
	defer func() { _ = db.Close() }()

	// Terminate connections first
	_, _ = db.Exec("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1", dbName)

	quoted := `"` + strings.ReplaceAll(dbName, `"`, `""`) + `"`
	if _, err := db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoted)); err != nil {
		fmt.Fprintf(os.Stderr, "  %s %s: %v\n", styleFail.Render("✗"), styleFail.Render(dbName), err)
	} else {
		fmt.Fprintf(os.Stderr, "  %s %s\n", styleSuccess.Render("✓"), styleMessage.Render(fmt.Sprintf("Dropped database %s", dbName)))
	}
}
