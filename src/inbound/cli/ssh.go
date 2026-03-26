package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/spf13/cobra"
)

func newSSHCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ssh <name>",
		Short: "Open an interactive SSH session to a remote preview environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]

			if globalMode != "remote" {
				return fmt.Errorf("ssh is only available in remote mode (use --mode remote or -m remote)")
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

			target := fmt.Sprintf("%s@%s", entry.Compute.User, entry.Compute.Host)
			Header(fmt.Sprintf("Connecting to %s", styleDetail.Render(envName)))
			KeyValue("Host", entry.Compute.Host)
			KeyValue("User", entry.Compute.User)
			KeyValue("Root", entry.Compute.Path)
			fmt.Println()

			// Replace the current process with ssh (interactive)
			sshBin, err := exec.LookPath("ssh")
			if err != nil {
				return fmt.Errorf("ssh not found: %w", err)
			}

			sshArgs := []string{"ssh", "-t", target, fmt.Sprintf("cd %s && exec $SHELL -l", entry.Compute.Path)}
			return syscall.Exec(sshBin, sshArgs, os.Environ())
		},
	}

	return cmd
}
