package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"charm.land/lipgloss/v2"

	"github.com/jake-landersweb/previewctl/src/domain"
	filestate "github.com/jake-landersweb/previewctl/src/outbound/state"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all environments (local and remote)",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := listAllEnvironments(cmd.Context())
			if err != nil {
				return err
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(entries)
			}

			if len(entries) == 0 {
				fmt.Fprintf(os.Stderr, "\n%s\n\n", styleDim.Render("No environments found."))
				return nil
			}

			headerStyle := lipgloss.NewStyle().
				Bold(true).
				Foreground(colorDim)

			nameStyle := lipgloss.NewStyle().
				Bold(true).
				Foreground(colorCyan)

			modeLocal := lipgloss.NewStyle().
				Foreground(colorBlue).
				Render("local")

			modeRemote := lipgloss.NewStyle().
				Foreground(colorMagenta).
				Render("remote")

			fmt.Println()
			for i, e := range entries {
				path := e.WorktreePath()

				mode := modeLocal
				if e.Mode == "remote" {
					mode = modeRemote
				}

				fmt.Fprintf(os.Stderr, "  %s  %s  %s  %s\n",
					nameStyle.Render(e.Name),
					headerStyle.Render("on"),
					styleMessage.Render(e.Branch),
					mode,
				)

				detail := ""
				if path != "" {
					detail = path
				} else if e.Compute != nil && e.Compute.Host != "" {
					detail = e.Compute.Host
				}

				fmt.Fprintf(os.Stderr, "    %s  %s  %s\n",
					StatusBadge(string(e.Status)),
					styleDim.Render("·"),
					styleDim.Render(detail),
				)
				if i < len(entries)-1 {
					fmt.Fprintln(os.Stderr)
				}
			}
			fmt.Fprintln(os.Stderr)

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}

// listAllEnvironments queries both local file state and remote Postgres state,
// deduplicates by name, and returns a merged sorted list.
func listAllEnvironments(ctx context.Context) ([]*domain.EnvironmentEntry, error) {
	seen := make(map[string]*domain.EnvironmentEntry)

	// Load base config for project name
	baseCfg, _, err := loadConfigWithMode("local")
	if err != nil {
		return nil, err
	}

	// Local file state
	home, _ := os.UserHomeDir()
	statePath := filepath.Join(home, ".cache", "previewctl", baseCfg.Name, "state.json")
	fileAdapter := filestate.NewFileStateAdapter(statePath)
	if localState, err := fileAdapter.Load(ctx); err == nil {
		for name, entry := range localState.Environments {
			seen[name] = entry
		}
	}

	// Remote Postgres state (if DSN available)
	dsn := os.Getenv("PREVIEWCTL_STATE_DSN")
	if dsn != "" {
		pgAdapter, err := filestate.NewPostgresStateAdapter(dsn, baseCfg.Name)
		if err == nil {
			if remoteState, err := pgAdapter.Load(ctx); err == nil {
				for name, entry := range remoteState.Environments {
					seen[name] = entry // remote wins on conflict
				}
			}
		}
	}

	// Sort by name
	entries := make([]*domain.EnvironmentEntry, 0, len(seen))
	for _, entry := range seen {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}
