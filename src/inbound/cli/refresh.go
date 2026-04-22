package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// allRunnerSteps defines all runner steps in execution order.
// Order must match Manager.BuildRunnerStepOrder in domain/manager.go so that
// `refresh --from <step>` selects the correct suffix of the canonical pipeline.
var allRunnerSteps = []string{
	"sync_code",
	"generate_manifest",
	"runner_before",
	"generate_env",
	"start_infra",
	"generate_compose",
	"generate_nginx",
	"build_services",
	"start_services",
	"runner_deploy",
	"runner_after",
}

// localSkipSteps are steps that are no-ops in local mode.
var localSkipSteps = map[string]bool{
	"sync_code": true,
}

func newRefreshCmd() *cobra.Command {
	var (
		only     string
		fromStep string
	)

	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Re-run steps after config or code changes",
		Long: `Refreshes an environment by re-running runner steps. Use this after
editing previewctl.yaml, pushing code, or when environment state needs
to be resynced.

By default, all runner steps are re-run without caching. Use --only to
run specific steps, or --from to re-run from a certain step onward.

Examples:
  previewctl refresh                           # re-run all steps
  previewctl refresh --only generate_env       # just regenerate env files
  previewctl refresh --from build_services     # rebuild and restart`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, cfg, err := buildManager(NewCLIProgressReporter())
			if err != nil {
				return err
			}

			home, _ := os.UserHomeDir()
			statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")

			envName, err := requireEnv(statePath)
			if err != nil {
				return fmt.Errorf("could not determine environment: %w", err)
			}

			// Always disable caching — the point of refresh is to pick up changes
			mgr.SetNoCache(true)

			// Determine which steps to run
			var steps []string
			switch {
			case only != "":
				steps = strings.Split(only, ",")
				for i := range steps {
					steps[i] = strings.TrimSpace(steps[i])
				}
				// Validate step names
				for _, s := range steps {
					if !runnerSteps[s] {
						return fmt.Errorf("unknown step '%s'", s)
					}
				}
			case fromStep != "":
				if !runnerSteps[fromStep] {
					return fmt.Errorf("unknown step '%s'", fromStep)
				}
				found := false
				for _, s := range allRunnerSteps {
					if s == fromStep {
						found = true
					}
					if found {
						steps = append(steps, s)
					}
				}
			default:
				steps = append([]string{}, allRunnerSteps...)
			}

			// Filter out steps that don't apply in local mode
			mode := resolvedMode()
			if mode == "local" {
				filtered := make([]string, 0, len(steps))
				for _, s := range steps {
					if !localSkipSteps[s] {
						filtered = append(filtered, s)
					}
				}
				steps = filtered
			}

			Header(fmt.Sprintf("Refreshing %s", styleDetail.Render(envName)))

			if err := mgr.RunSteps(cmd.Context(), envName, steps); err != nil {
				return err
			}

			Success(fmt.Sprintf("Environment %s refreshed", styleDetail.Render(envName)))
			return nil
		},
	}

	cmd.Flags().StringVar(&only, "only", "", "Run only these steps (comma-separated)")
	cmd.Flags().StringVar(&fromStep, "from", "", "Re-run from this step onward")

	return cmd
}
