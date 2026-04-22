package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

			var proxyDomain string
			if cfg.Runner != nil && cfg.Runner.Compose != nil && cfg.Runner.Compose.Proxy != nil {
				proxyDomain = cfg.Runner.Compose.Proxy.Domain
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
					if proxyDomain != "" {
						host := fmt.Sprintf("%s--%s.%s", e.Name, name, proxyDomain)
						url = fmt.Sprintf("[%s](https://%s)", host, host)
					} else {
						url = fmt.Sprintf("http://localhost:%d", e.Ports[name])
					}
					fmt.Printf("| %s | %s |\n", name, url)
				}
				return nil
			}

			// Try to fetch per-container status from docker compose. This is best-effort:
			// if the compute is unreachable or the entry has no compute info yet we just
			// render the summary without per-service status.
			var byService map[string]composePsEntry
			if cfg.Infrastructure != nil && cfg.Infrastructure.ComposeFile != "" {
				if t, err := buildInfraTarget(mgr, cfg, e, envName); err == nil {
					if ps, err := fetchComposePs(cmd.Context(), t); err == nil {
						byService = ps
					}
				}
			}

			// Default pretty format
			Header(fmt.Sprintf("Environment %s", styleDetail.Render(e.Name)))

			KeyValue("Branch", e.Branch)
			KeyValue("Mode", string(e.Mode))
			KeyValue("Status", StatusBadge(string(e.Status)))

			if wt := e.WorktreePath(); wt != "" {
				KeyValue("Worktree", wt)
			}
			if e.Compute != nil && e.Compute.Type == "ssh" && e.Compute.Host != "" {
				KeyValue("Host", e.Compute.Host)
			}

			// Services — ordered by port entry, excluding infra services.
			fmt.Fprintln(os.Stderr)
			SectionHeader("Services")
			svcNames := make([]string, 0, len(e.Ports))
			for name := range e.Ports {
				if infraSet[name] {
					continue
				}
				svcNames = append(svcNames, name)
			}
			sort.Strings(svcNames)
			if len(svcNames) == 0 {
				fmt.Fprintf(os.Stderr, "    %s\n", styleDim.Render("(none)"))
			} else {
				width := longestName(svcNames)
				for _, name := range svcNames {
					port := e.Ports[name]
					var url string
					if proxyDomain != "" {
						url = fmt.Sprintf("https://%s--%s.%s", e.Name, name, proxyDomain)
					} else {
						url = fmt.Sprintf("http://localhost:%d", port)
					}
					badge := serviceBadge(byService, name)
					fmt.Fprintf(os.Stderr, "    %s  %s  %s\n",
						styleDim.Render(padRight(name, width)),
						badge,
						styleDetail.Render(url),
					)
				}
			}

			// Infrastructure — per-service status from `docker compose ps`.
			if len(cfg.InfraServices) > 0 {
				fmt.Fprintln(os.Stderr)
				SectionHeader("Infrastructure")
				infraNames := make([]string, 0, len(cfg.InfraServices))
				for name := range cfg.InfraServices {
					infraNames = append(infraNames, name)
				}
				sort.Strings(infraNames)
				width := longestName(infraNames)
				for _, name := range infraNames {
					badge, detailText := infraBadgeAndDetail(byService, name)
					fmt.Fprintf(os.Stderr, "    %s  %s  %s\n",
						styleDim.Render(padRight(name, width)),
						badge,
						styleDim.Render(detailText),
					)
				}
			}

			fmt.Fprintln(os.Stderr)

			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "", "Output format: markdown")

	return cmd
}

// serviceBadge returns a status badge for a runner service by consulting the
// compose ps map. If the service has no container, it renders as "stopped".
func serviceBadge(byService map[string]composePsEntry, name string) string {
	if byService == nil {
		return StatusBadge("unknown")
	}
	e, found := byService[name]
	if !found {
		return StatusBadge("stopped")
	}
	if strings.EqualFold(e.State, "running") {
		return StatusBadge("running")
	}
	return StatusBadge("stopped")
}

// infraBadgeAndDetail returns a status badge plus the docker-reported detail
// string (e.g. "Up 16 hours (healthy)") for an infra service.
func infraBadgeAndDetail(byService map[string]composePsEntry, name string) (string, string) {
	if byService == nil {
		return StatusBadge("unknown"), "compute unreachable"
	}
	e, found := byService[name]
	if !found {
		return StatusBadge("missing"), "not created"
	}
	if strings.EqualFold(e.State, "running") {
		return StatusBadge("running"), e.Status
	}
	detail := e.Status
	if detail == "" {
		detail = e.State
	}
	return StatusBadge("stopped"), detail
}

func longestName(names []string) int {
	w := 0
	for _, n := range names {
		if len(n) > w {
			w = len(n)
		}
	}
	return w
}
