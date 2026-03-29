package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newReconcileCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Verify and heal runner steps for an environment",
		Long: `Walks all runner steps, verifies their side effects still exist, and
re-executes any that fail verification. Hook-owned steps (runner_before,
runner_deploy, runner_after) are skipped since previewctl cannot verify
user-defined hooks.

Use --dry-run to check health without making any changes.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := globalEnvName
			if envName == "" {
				return fmt.Errorf("--env (-e) is required for reconcile")
			}

			if resolvedMode() != "remote" {
				return fmt.Errorf("reconcile is only available for remote environments")
			}

			progress := NewCLIProgressReporter()
			mgr, _, err := buildManager(progress)
			if err != nil {
				return err
			}

			if dryRun {
				Header(fmt.Sprintf("Checking %s (dry run)", styleDetail.Render(envName)))
			} else {
				Header(fmt.Sprintf("Reconciling %s", styleDetail.Render(envName)))
			}

			report, err := mgr.Reconcile(cmd.Context(), envName, dryRun)
			if err != nil {
				return err
			}

			// Summary
			fmt.Fprintln(os.Stderr)
			SectionHeader("Summary")
			if report.OK > 0 {
				DetailKeyValue("Healthy", fmt.Sprintf("%d", report.OK))
			}
			if !dryRun && report.Healed > 0 {
				DetailKeyValue("Healed", fmt.Sprintf("%d", report.Healed))
			}
			if report.Failed > 0 {
				if dryRun {
					DetailKeyValue("Broken", fmt.Sprintf("%d", report.Failed))
				} else {
					DetailKeyValue("Failed", fmt.Sprintf("%d", report.Failed))
				}
			}
			if report.Skipped > 0 {
				DetailKeyValue("Skipped", fmt.Sprintf("%d (hook-owned)", report.Skipped))
			}
			if report.NotRun > 0 {
				DetailKeyValue("Not run", fmt.Sprintf("%d (never completed)", report.NotRun))
			}
			fmt.Fprintln(os.Stderr)

			if dryRun {
				if report.Failed > 0 {
					return fmt.Errorf("%d step(s) need healing", report.Failed)
				}
				Success("All steps healthy")
				return nil
			}

			if report.Failed > 0 {
				return fmt.Errorf("%d step(s) could not be healed", report.Failed)
			}
			if report.Healed > 0 {
				Success(fmt.Sprintf("Healed %d step(s)", report.Healed))
			} else {
				Success("All steps healthy")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Check health without making changes")

	return cmd
}
