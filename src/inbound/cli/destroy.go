package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newDestroyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy [name]",
		Short: "Tear down an environment",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			progress := NewCLIProgressReporter()
			mgr, cfg, err := buildManager(progress)
			if err != nil {
				return err
			}

			home, _ := os.UserHomeDir()
			statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")

			envName, err := resolveEnvName(args, statePath)
			if err != nil {
				return fmt.Errorf("could not determine environment: %w", err)
			}

			fmt.Printf("\nDestroying environment '%s'\n\n", envName)

			if err := mgr.Destroy(cmd.Context(), envName); err != nil {
				return err
			}

			fmt.Printf("\nEnvironment '%s' destroyed.\n\n", envName)
			return nil
		},
	}

	return cmd
}
