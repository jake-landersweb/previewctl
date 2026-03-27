package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// runnerSteps lists valid steps for isolated execution.
var runnerSteps = map[string]bool{
	"sync_code":        true,
	"runner_before":    true,
	"generate_env":     true,
	"start_infra":      true,
	"generate_compose": true,
	"generate_nginx":   true,
	"build_services":   true,
	"start_services":   true,
	"runner_deploy":    true,
	"runner_after":     true,
}

func newStepCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "step <step-name>",
		Short: "Re-run a single runner-phase step in isolation",
		Long: `Executes a single runner-phase step without running the full lifecycle.
Useful for regenerating config files, restarting services, or re-running
migrations without reprovisioning.

Available steps:
  sync_code        - Pull latest code from remote
  runner_before    - Re-run the setup hook
  generate_env     - Regenerate .env files from manifest
  start_infra      - Restart infrastructure containers
  generate_compose - Regenerate Docker Compose file
  generate_nginx   - Regenerate nginx config
  build_services   - Rebuild autostart services
  start_services   - Restart autostart services
  runner_deploy    - Re-run the deploy hook
  runner_after     - Re-run the after hook (e.g., migrations)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stepName := args[0]

			if globalMode != "remote" {
				return fmt.Errorf("step command is only available in remote mode (use -m remote)")
			}

			envName := globalEnvName
			if envName == "" {
				return fmt.Errorf("--env (-e) is required for remote mode")
			}

			if !runnerSteps[stepName] {
				return fmt.Errorf("unknown runner step '%s'", stepName)
			}

			progress := NewCLIProgressReporter()
			mgr, _, err := buildManager(progress)
			if err != nil {
				return err
			}

			Header(fmt.Sprintf("Running step %s on %s",
				styleDetail.Render(stepName),
				styleDetail.Render(envName)))

			if err := mgr.RunStep(cmd.Context(), envName, stepName); err != nil {
				return err
			}

			Success(fmt.Sprintf("Step %s completed", styleDetail.Render(stepName)))
			return nil
		},
	}

	return cmd
}
