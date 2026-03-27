package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

func newCreateCmd() *cobra.Command {
	var (
		branch  string
		noCache bool
	)

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new development environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]
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

			// Show service URLs
			var domain string
			if cfg.Runner != nil && cfg.Runner.Compose != nil && cfg.Runner.Compose.Proxy != nil {
				domain = cfg.Runner.Compose.Proxy.Domain
			}

			SectionHeader("Services")
			portNames := make([]string, 0, len(entry.Ports))
			for name := range entry.Ports {
				portNames = append(portNames, name)
			}
			sort.Strings(portNames)
			for _, name := range portNames {
				port := entry.Ports[name]
				var url string
				if domain != "" {
					url = fmt.Sprintf("https://%s--%s.%s", envName, name, domain)
				} else {
					url = fmt.Sprintf("http://localhost:%d", port)
				}
				DetailKeyValue(name, url)
			}
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Git branch name (defaults to environment name)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Skip all step caching, re-run everything")

	return cmd
}
