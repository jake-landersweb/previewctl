package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

// newSyncCmd reconciles the remote VM with current state: regenerates
// manifest/env/compose/nginx and restarts every currently-enabled service.
//
// Unlike `refresh`, it does not re-run runner_before, build_services, or
// runner_deploy — those rebuild code on the VM. `sync` only pushes state
// changes (creds, store values, enabled-service set) down to the VM.
//
// Typical uses:
//   - Recovering when `core <svc> reset --no-propagate` was used or when
//     automatic propagation failed mid-flight.
//   - Applying a store value edited with `store set` to running services.
func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Push current state to the remote VM (regen configs + restart services)",
		Long: `Reconciles the remote VM with current state without rebuilding code.

Regenerates .previewctl.json, .env/.env.local, .previewctl.compose.yaml, and
nginx config from current state, then restarts every currently-enabled service
so they observe the new values.

Use after 'core <svc> reset --no-propagate', 'store set', or any time the
VM has drifted from state.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			progress := NewCLIProgressReporter()
			mgr, cfg, err := buildManager(progress)
			if err != nil {
				return err
			}

			home, _ := os.UserHomeDir()
			statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")

			envName, err := requireEnv(statePath)
			if err != nil {
				return fmt.Errorf("could not determine environment: %w", err)
			}

			Header(fmt.Sprintf("Syncing %s", styleDetail.Render(envName)))
			if err := mgr.SyncRemote(cmd.Context(), envName); err != nil {
				return err
			}

			Success(fmt.Sprintf("Environment %s synced", styleDetail.Render(envName)))
			return nil
		},
	}
}
