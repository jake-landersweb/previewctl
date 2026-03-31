package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
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

			// Provisioner outputs omitted from status — may contain credentials.

			var domain string
			if cfg.Runner != nil && cfg.Runner.Compose != nil && cfg.Runner.Compose.Proxy != nil {
				domain = cfg.Runner.Compose.Proxy.Domain
			}

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

	return cmd
}
