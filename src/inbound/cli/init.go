package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var branch string

	cmd := &cobra.Command{
		Use:   "init <name>",
		Short: "Create a new local development environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]
			if branch == "" {
				branch = envName
			}

			progress := NewCLIProgressReporter()
			mgr, _, err := buildManager(progress)
			if err != nil {
				return err
			}

			fmt.Printf("\nCreating environment '%s' (branch: %s)\n\n", envName, branch)

			entry, err := mgr.Init(cmd.Context(), envName, branch)
			if err != nil {
				return err
			}

			fmt.Printf("\nEnvironment '%s' is ready!\n", entry.Name)
			fmt.Printf("  Worktree: %s\n", entry.Local.WorktreePath)
			fmt.Printf("  Ports:\n")
			for svc, port := range entry.Ports {
				fmt.Printf("    %s: %d\n", svc, port)
			}
			for dbName, dbRef := range entry.Databases {
				fmt.Printf("  Database (%s): %s\n", dbName, dbRef.Name)
			}
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Git branch name (defaults to environment name)")

	return cmd
}
