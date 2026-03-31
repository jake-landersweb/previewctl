package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show detailed environment status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, cfg, err := buildManager(nil)
			if err != nil {
				return err
			}

			home, _ := os.UserHomeDir()
			statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")

			envName, err := requireEnv(statePath)
			if err != nil {
				return fmt.Errorf("could not determine environment: %w", err)
			}

			detail, err := mgr.Status(cmd.Context(), envName)
			if err != nil {
				return err
			}

			e := detail.Entry

			var domain string
			if cfg.Runner != nil && cfg.Runner.Compose != nil && cfg.Runner.Compose.Proxy != nil {
				domain = cfg.Runner.Compose.Proxy.Domain
			}

			infraSet := make(map[string]bool)
			for name := range cfg.InfraServices {
				infraSet[name] = true
			}

			// Markdown format: outputs a markdown table to stdout for embedding in PRs.
			if format == "markdown" {
				fmt.Println("| Service | URL |")
				fmt.Println("|---|---|")

				portNames := make([]string, 0, len(e.Ports))
				for name := range e.Ports {
					if infraSet[name] {
						continue
					}
					portNames = append(portNames, name)
				}
				sort.Strings(portNames)

				for _, name := range portNames {
					var url string
					if domain != "" {
						host := fmt.Sprintf("%s--%s.%s", e.Name, name, domain)
						url = fmt.Sprintf("[%s](https://%s)", host, host)
					} else {
						url = fmt.Sprintf("http://localhost:%d", e.Ports[name])
					}
					fmt.Printf("| %s | %s |\n", name, url)
				}
				return nil
			}

			// Default pretty format
			Header(fmt.Sprintf("Environment %s", styleDetail.Render(e.Name)))

			KeyValue("Branch", e.Branch)
			KeyValue("Mode", string(e.Mode))
			KeyValue("Status", StatusBadge(string(e.Status)))

			if wt := e.WorktreePath(); wt != "" {
				KeyValue("Worktree", wt)
			}

			infraStatus := "stopped"
			if detail.InfraRunning {
				infraStatus = "running"
			}
			KeyValue("Infrastructure", StatusBadge(infraStatus))

			var infraNames []string
			for name := range cfg.InfraServices {
				infraNames = append(infraNames, name)
			}

			fmt.Fprintln(os.Stderr)
			PrintServiceURLs(e.Name, e.Ports, domain, infraNames...)
			fmt.Fprintln(os.Stderr)

			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "", "Output format: markdown")

	return cmd
}
