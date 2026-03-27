package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

func newProvisionCmd() *cobra.Command {
	var (
		branch   string
		fromStep string
		noCache  bool
	)

	cmd := &cobra.Command{
		Use:   "provision",
		Short: "Run the provisioner phase only (create compute, seed services, write manifest)",
		Long: `Provision sets up compute resources and external services for an environment
without running the runner phase. This is used in CI/remote workflows where
provisioning happens on the orchestrator and running happens on the VM.

After provisioning, the environment is in "provisioned" state. Use 'previewctl run'
on the target compute to execute the runner phase.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := globalEnvName
			if envName == "" {
				return fmt.Errorf("--env (-e) is required for provision")
			}
			if branch == "" {
				branch = envName
			}

			progress := NewCLIProgressReporter()
			mgr, _, err := buildManager(progress)
			if err != nil {
				return err
			}
			if noCache {
				mgr.SetNoCache(true)
			}

			Header(fmt.Sprintf("Provisioning environment %s",
				styleDetail.Render(envName)))

			entry, err := mgr.Provision(cmd.Context(), envName, branch, fromStep)
			if err != nil {
				return err
			}

			Success(fmt.Sprintf("Environment %s provisioned",
				styleDetail.Render(entry.Name)))

			KeyValue("Branch", entry.Branch)
			KeyValue("Status", StatusBadge(string(entry.Status)))
			if wt := entry.WorktreePath(); wt != "" {
				KeyValue("Worktree", wt)
			}
			if entry.Compute != nil && entry.Compute.Host != "" {
				KeyValue("Host", entry.Compute.Host)
			}

			SectionHeader("Ports")
			portNames := make([]string, 0, len(entry.Ports))
			for name := range entry.Ports {
				portNames = append(portNames, name)
			}
			sort.Strings(portNames)
			for _, name := range portNames {
				DetailKeyValue(name, fmt.Sprintf("%d", entry.Ports[name]))
			}
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVarP(&branch, "branch", "b", "", "Git branch name (defaults to environment name)")
	cmd.Flags().StringVar(&fromStep, "from", "", "Force re-run from this step (invalidates subsequent steps)")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Skip all step caching, re-run everything")

	return cmd
}
