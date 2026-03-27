package cli

import (
	"github.com/spf13/cobra"
)

func newEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environments",
		Long: `Create, inspect, and operate on preview and development environments.
All env subcommands operate on a specific environment identified by
the --env (-e) flag.`,
	}

	cmd.AddCommand(
		newCreateCmd(),
		newDeleteCmd(),
		newListCmd(),
		newStatusCmd(),
		newSSHCmd(),
		newServiceCmd(),
		newRefreshCmd(),
		newReconcileCmd(),
		newStepsCmd(),
		newStoreCmd(),
		newRunGroupCmd(),
		newProvisionerCmd(),
	)

	return cmd
}

func newRunGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute lifecycle phases",
		Long: `Run individual lifecycle phases for an environment. These are
advanced operations — most users should use 'env create' instead.`,
	}

	cmd.AddCommand(
		newProvisionCmd(),
		newStepCmd(),
		newRunnerCmd(),
	)

	return cmd
}
