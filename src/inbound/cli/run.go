package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newRunGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute lifecycle phases",
		Long: `Run individual lifecycle phases for an environment. These are
advanced operations — most users should use 'create' or 'refresh' instead.`,
	}

	cmd.AddCommand(
		newProvisionCmd(),
		newRunnerCmd(),
	)

	return cmd
}

func newRunnerCmd() *cobra.Command {
	var (
		manifestPath string
		fromStep     string
		noCache      bool
	)

	cmd := &cobra.Command{
		Use:   "runner",
		Short: "Run the runner phase from an existing manifest",
		Long: `Run executes the runner phase (install deps, generate env files, start
infrastructure, deploy) using a .previewctl.json manifest. This is typically
used on a VM after 'previewctl provision' has been run on CI.

The manifest must already exist at the target location. The runner reads
hook configuration from previewctl.yaml in the current repo.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve manifest path
			if manifestPath == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting cwd: %w", err)
				}
				manifestPath = filepath.Join(cwd, ".previewctl.json")
			}
			absPath, err := filepath.Abs(manifestPath)
			if err != nil {
				return fmt.Errorf("resolving manifest path: %w", err)
			}

			// Verify manifest exists
			if _, err := os.Stat(absPath); err != nil {
				return fmt.Errorf("manifest not found: %s", absPath)
			}

			progress := NewCLIProgressReporter()
			mgr, _, err := buildManager(progress)
			if err != nil {
				return err
			}
			if noCache {
				mgr.SetNoCache(true)
			}

			Header("Running from manifest")
			KeyValue("Manifest", absPath)

			if err := mgr.Run(cmd.Context(), absPath, fromStep); err != nil {
				return err
			}

			Success("Runner phase complete")
			return nil
		},
	}

	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Path to .previewctl.json (defaults to ./.previewctl.json)")
	cmd.Flags().StringVar(&fromStep, "from", "", "Force re-run from this step (invalidates subsequent steps)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Skip all step caching, re-run everything")

	return cmd
}
