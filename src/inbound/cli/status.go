package cli

import (
	"fmt"
	"os"
	"path/filepath"

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
			fmt.Printf("\nEnvironment: %s\n", e.Name)
			fmt.Printf("  Branch:   %s\n", e.Branch)
			fmt.Printf("  Mode:     %s\n", e.Mode)
			fmt.Printf("  Status:   %s\n", e.Status)

			if e.Local != nil {
				fmt.Printf("  Worktree: %s\n", e.Local.WorktreePath)
			}

			infraStatus := "stopped"
			if detail.InfraRunning {
				infraStatus = "running"
			}
			fmt.Printf("  Infra:    %s\n", infraStatus)

			fmt.Println("  Databases:")
			for name, exists := range detail.DatabaseExists {
				status := "missing"
				if exists {
					status = "exists"
				}
				dbRef := e.Databases[name]
				fmt.Printf("    %s (%s): %s\n", name, dbRef.Name, status)
			}

			fmt.Println("  Ports:")
			for svc, port := range e.Ports {
				fmt.Printf("    %s: %d\n", svc, port)
			}
			fmt.Println()

			return nil
		},
	}

	return cmd
}
