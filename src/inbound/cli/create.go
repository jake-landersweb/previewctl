package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

func newCreateCmd() *cobra.Command {
	var branch string

	cmd := &cobra.Command{
		Use:   "create <name>",
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

			Header(fmt.Sprintf("Creating environment %s",
				styleDetail.Render(envName)))

			entry, err := mgr.Init(cmd.Context(), envName, branch)
			if err != nil {
				return err
			}

			Success(fmt.Sprintf("Environment %s is ready",
				styleDetail.Render(entry.Name)))

			KeyValue("Branch", entry.Branch)
			if entry.Local != nil {
				KeyValue("Worktree", entry.Local.WorktreePath)
			}

			SectionHeader("Ports")
			// Sort port names for consistent output
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

	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Git branch name (defaults to environment name)")

	return cmd
}
