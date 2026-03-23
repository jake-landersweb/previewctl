package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	var mode string

	cmd := &cobra.Command{
		Use:   "delete [name]",
		Short: "Delete an environment",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			progress := NewCLIProgressReporter()
			mgr, cfg, err := buildManagerWithMode(progress, mode)
			if err != nil {
				return err
			}

			home, _ := os.UserHomeDir()
			statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")

			envName, err := resolveEnvName(args, statePath)
			if err != nil {
				return fmt.Errorf("could not determine environment: %w", err)
			}

			Header(fmt.Sprintf("Deleting environment %s",
				styleDetail.Render(envName)))

			if err := mgr.Destroy(cmd.Context(), envName); err != nil {
				return err
			}

			Success(fmt.Sprintf("Environment %s deleted",
				styleDetail.Render(envName)))

			return nil
		},
	}

	cmd.Flags().StringVarP(&mode, "mode", "m", "local", "Deployment mode (local, remote)")

	return cmd
}
