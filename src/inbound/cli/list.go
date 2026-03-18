package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

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
				fmt.Println("No environments found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tBRANCH\tSTATUS\tMODE\tPATH")
			for _, e := range entries {
				path := ""
				if e.Local != nil {
					path = e.Local.WorktreePath
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Name, e.Branch, e.Status, e.Mode, path)
			}
			w.Flush()

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}
