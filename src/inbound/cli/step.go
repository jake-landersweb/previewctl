package cli

import (
	"fmt"
	"os"

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
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "step <step-name>",
		Short: "Re-run a single runner-phase step in isolation",
		Long: `Executes a single runner-phase step without running the full lifecycle.
Useful for regenerating config files, restarting services, or re-running
migrations without reprovisioning.

Use --dry-run to preview what a step would do (shows generated config
for generation steps, or describes the action for other steps).

Available steps:
  sync_code        - Pull latest code from remote
  runner_before    - Re-run the setup hook
  generate_env     - Regenerate .env files from manifest
  start_infra      - Restart infrastructure containers
  generate_compose - Regenerate Docker Compose file
  generate_nginx   - Regenerate nginx config
  build_services   - Rebuild enabled services
  start_services   - Restart enabled services
  runner_deploy    - Re-run the deploy hook
  runner_after     - Re-run the after hook (e.g., migrations)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stepName := args[0]

			envName := globalEnvName
			if envName == "" {
				return fmt.Errorf("--env (-e) is required for step")
			}

			if resolvedMode() != "remote" {
				return fmt.Errorf("step command is only available for remote environments")
			}

			if !runnerSteps[stepName] {
				return fmt.Errorf("unknown runner step '%s'", stepName)
			}

			if dryRun {
				mgr, _, err := buildManager(nil)
				if err != nil {
					return err
				}

				Header(fmt.Sprintf("Dry run: %s on %s",
					styleDetail.Render(stepName),
					styleDetail.Render(envName)))

				output, err := mgr.DryRunStep(cmd.Context(), envName, stepName)
				if err != nil {
					return err
				}

				fmt.Fprint(os.Stdout, output)
				return nil
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

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview what the step would do without executing")

	return cmd
}
