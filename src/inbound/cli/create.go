package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCreateCmd() *cobra.Command {
	var (
		branch  string
		noCache bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new development environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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

	return cmd
}
