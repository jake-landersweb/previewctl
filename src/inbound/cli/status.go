package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [name]",
		Short: "Show detailed environment status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, cfg, err := buildManager(nil)
			if err != nil {
				return err
			}

			home, _ := os.UserHomeDir()
			statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")

			envName, err := resolveEnvName(args, statePath)
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

			if e.Local != nil {
				KeyValue("Worktree", e.Local.WorktreePath)
			}

			infraStatus := "stopped"
			if detail.InfraRunning {
				infraStatus = "running"
			}
			KeyValue("Infrastructure", StatusBadge(infraStatus))

			// Display provisioner outputs if any
			if len(e.ProvisionerOutputs) > 0 {
				fmt.Fprintln(os.Stderr)
				SectionHeader("Provisioner Outputs")
				for svcName, outputs := range e.ProvisionerOutputs {
					for key, val := range outputs {
						DetailKeyValue(fmt.Sprintf("%s.%s", svcName, key), val)
					}
				}
			}

			fmt.Fprintln(os.Stderr)
			SectionHeader("Ports")
			portNames := make([]string, 0, len(e.Ports))
			for name := range e.Ports {
				portNames = append(portNames, name)
			}
			sort.Strings(portNames)
			for _, name := range portNames {
				DetailKeyValue(name, fmt.Sprintf("%d", e.Ports[name]))
			}
			fmt.Fprintln(os.Stderr)

			return nil
		},
	}

	return cmd
}
