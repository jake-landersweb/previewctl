package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"charm.land/lipgloss/v2"

	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all environments",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, _, err := buildManager(nil)
			if err != nil {
				return err
			}

			entries, err := mgr.List(cmd.Context())
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

			fmt.Println()
			for i, e := range entries {
				path := ""
				if e.Local != nil {
					path = e.Local.WorktreePath
				}

				fmt.Fprintf(os.Stderr, "  %s  %s  %s\n",
					nameStyle.Render(e.Name),
					headerStyle.Render("on"),
					styleMessage.Render(e.Branch),
				)
				fmt.Fprintf(os.Stderr, "    %s  %s  %s\n",
					StatusBadge(string(e.Status)),
					styleDim.Render("·"),
					styleDim.Render(path),
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
