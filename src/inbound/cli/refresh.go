package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// refreshSteps defines the steps executed during a refresh, in order.
var refreshSteps = []string{
	"sync_code",
	"generate_manifest",
	"generate_env",
	"build_services",
	"start_services",
	"generate_nginx",
}

func newRefreshCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Sync code, rebuild, and restart all active services",
		Long: `Refreshes a remote preview environment after code changes. Runs the
following steps in order:

  1. sync_code          - Pull latest code from the branch
  2. generate_manifest  - Rebuild .previewctl.json from current config
  3. generate_env       - Regenerate .env files from manifest
  4. build_services     - Rebuild all enabled services
  5. start_services     - Restart all enabled services
  6. generate_nginx     - Regenerate and reload nginx config

This is the standard operation after pushing code to a branch that
has a preview environment attached.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := globalEnvName
			if envName == "" {
				return fmt.Errorf("--env (-e) is required for refresh")
			}

			if resolvedMode() != "remote" {
				return fmt.Errorf("refresh is only available for remote environments")
			}

			progress := NewCLIProgressReporter()
			mgr, _, err := buildManager(progress)
			if err != nil {
				return err
			}

			Header(fmt.Sprintf("Refreshing %s", styleDetail.Render(envName)))

			if err := mgr.RunSteps(cmd.Context(), envName, refreshSteps); err != nil {
				return err
			}

			Success(fmt.Sprintf("Environment %s refreshed", styleDetail.Render(envName)))
			return nil
		},
	}
}
