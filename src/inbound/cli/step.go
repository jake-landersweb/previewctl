package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// runnerSteps lists valid steps for isolated execution.
var runnerSteps = map[string]bool{
	"sync_code":         true,
	"generate_manifest": true,
	"runner_before":     true,
	"generate_env":      true,
	"start_infra":       true,
	"generate_compose":  true,
	"generate_nginx":    true,
	"build_services":    true,
	"start_services":    true,
	"runner_deploy":     true,
	"runner_after":      true,
}

func newStepCmd() *cobra.Command {
	var (
		dryRun    bool
		printFlag bool
	)

	cmd := &cobra.Command{
		Use:   "step <step-name>",
		Short: "Re-run a single runner-phase step in isolation",
		Long: `Executes a single runner-phase step without running the full lifecycle.
Useful for regenerating config files, restarting services, or re-running
migrations without reprovisioning.

Use --dry-run to show a diff of what would change.
Use --print to output the full generated content (for generation steps).

Available steps:
  sync_code          - Pull latest code from remote
  generate_manifest  - Rebuild .previewctl.json from current config
  runner_before      - Re-run the setup hook
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

			// Print mode: dump full generated content
			if printFlag {
				mgr, _, err := buildManager(nil)
				if err != nil {
					return err
				}
				output, err := mgr.PrintStep(cmd.Context(), envName, stepName)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprint(os.Stdout, output)
				return nil
			}

			// Dry run mode: show diff
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

				_, _ = fmt.Fprint(os.Stdout, output)
				return nil
			}

			// Execute mode
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

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show a diff of what would change without executing")
	cmd.Flags().BoolVar(&printFlag, "print", false, "Print the full generated content to stdout")

	return cmd
}
