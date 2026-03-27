package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/jake-landersweb/previewctl/src/domain"
	"github.com/spf13/cobra"
)

func newSSHCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ssh",
		Short: "Open an interactive SSH session to a remote preview environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := globalEnvName
			if envName == "" {
				return fmt.Errorf("--env (-e) is required for ssh")
			}

			if resolvedMode() != "remote" {
				return fmt.Errorf("ssh is only available for remote environments (use -m remote for create, or the environment must be remote)")
			}

			mgr, _, err := buildManager(nil)
			if err != nil {
				return err
			}

			entry, err := mgr.GetEnvironment(cmd.Context(), envName)
			if err != nil {
				return fmt.Errorf("loading environment: %w", err)
			}
			if entry == nil {
				return fmt.Errorf("environment '%s' not found", envName)
			}
			if entry.Compute == nil || entry.Compute.Type != "ssh" {
				return fmt.Errorf("environment '%s' is not a remote environment", envName)
			}

			ca := mgr.BuildSSHComputeAccess(entry)
			sshCA, ok := ca.(*domain.DomainSSHComputeAccess)
			if !ok {
				return fmt.Errorf("environment '%s' does not support SSH", envName)
			}

			Header(fmt.Sprintf("Connecting to %s", styleDetail.Render(envName)))
			KeyValue("Host", entry.Compute.Host)
			KeyValue("User", sshCA.User())
			KeyValue("Root", sshCA.Root())
			fmt.Println()

			// Replace the current process with ssh (interactive)
			sshBin, err := exec.LookPath("ssh")
			if err != nil {
				return fmt.Errorf("ssh not found: %w", err)
			}

			sshArgs := append([]string{"ssh", "-t"}, sshCA.SSHArgs()...)
			sshArgs = append(sshArgs, fmt.Sprintf("cd %s && exec $SHELL -l", sshCA.Root()))
			return syscall.Exec(sshBin, sshArgs, os.Environ())
		},
	}

	return cmd
}
