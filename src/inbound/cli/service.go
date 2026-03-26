package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/jake-landersweb/previewctl/src/domain"
	"github.com/spf13/cobra"
)

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage services in a remote preview environment",
		Long: `Start, stop, restart, and inspect individual services in a remote
preview environment. Requires --mode remote.`,
	}

	cmd.AddCommand(
		newServiceStartCmd(),
		newServiceStopCmd(),
		newServiceRestartCmd(),
		newServiceLogsCmd(),
		newServiceListCmd(),
	)

	return cmd
}

func newServiceStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <env> <service>",
		Short: "Build and start a service",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName, svcName := args[0], args[1]
			ca, cfg, err := resolveRemoteEnv(cmd, envName)
			if err != nil {
				return err
			}

			svc, ok := cfg.Services[svcName]
			if !ok {
				return fmt.Errorf("unknown service '%s'", svcName)
			}

			// Build if configured
			if svc.Build != "" {
				Header(fmt.Sprintf("Building %s", styleDetail.Render(svcName)))
				if _, err := ca.Exec(cmd.Context(), svc.Build, nil); err != nil {
					return fmt.Errorf("building service: %w", err)
				}
			}

			// Start via compose
			Header(fmt.Sprintf("Starting %s", styleDetail.Render(svcName)))
			composeCmd := fmt.Sprintf("docker compose -f .previewctl.compose.yaml up -d %s", svcName)
			if _, err := ca.Exec(cmd.Context(), composeCmd, nil); err != nil {
				return fmt.Errorf("starting service: %w", err)
			}

			Success(fmt.Sprintf("Service %s started", styleDetail.Render(svcName)))
			return nil
		},
	}
}

func newServiceStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <env> <service>",
		Short: "Stop a running service",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName, svcName := args[0], args[1]
			ca, _, err := resolveRemoteEnv(cmd, envName)
			if err != nil {
				return err
			}

			Header(fmt.Sprintf("Stopping %s", styleDetail.Render(svcName)))
			composeCmd := fmt.Sprintf("docker compose -f .previewctl.compose.yaml stop %s", svcName)
			if _, err := ca.Exec(cmd.Context(), composeCmd, nil); err != nil {
				return fmt.Errorf("stopping service: %w", err)
			}

			Success(fmt.Sprintf("Service %s stopped", styleDetail.Render(svcName)))
			return nil
		},
	}
}

func newServiceRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart <env> <service>",
		Short: "Rebuild and restart a service",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName, svcName := args[0], args[1]
			ca, cfg, err := resolveRemoteEnv(cmd, envName)
			if err != nil {
				return err
			}

			svc, ok := cfg.Services[svcName]
			if !ok {
				return fmt.Errorf("unknown service '%s'", svcName)
			}

			// Rebuild if configured
			if svc.Build != "" {
				Header(fmt.Sprintf("Building %s", styleDetail.Render(svcName)))
				if _, err := ca.Exec(cmd.Context(), svc.Build, nil); err != nil {
					return fmt.Errorf("building service: %w", err)
				}
			}

			Header(fmt.Sprintf("Restarting %s", styleDetail.Render(svcName)))
			composeCmd := fmt.Sprintf("docker compose -f .previewctl.compose.yaml restart %s", svcName)
			if _, err := ca.Exec(cmd.Context(), composeCmd, nil); err != nil {
				return fmt.Errorf("restarting service: %w", err)
			}

			Success(fmt.Sprintf("Service %s restarted", styleDetail.Render(svcName)))
			return nil
		},
	}
}

func newServiceLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs <env> [service]",
		Short: "Stream service logs",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]
			ca, _, err := resolveRemoteEnv(cmd, envName)
			if err != nil {
				return err
			}

			svcArg := ""
			if len(args) > 1 {
				svcArg = args[1]
			}

			// Use syscall.Exec to replace the process for interactive log streaming
			entry, err := getRemoteEntry(cmd, envName)
			if err != nil {
				return err
			}

			composeCmd := fmt.Sprintf("docker compose -f .previewctl.compose.yaml logs -f %s", svcArg)
			remoteCmd := fmt.Sprintf("cd %q && %s", ca.Root(), composeCmd)

			sshBin, err := exec.LookPath("ssh")
			if err != nil {
				return fmt.Errorf("ssh not found: %w", err)
			}

			target := fmt.Sprintf("%s@%s", entry.Compute.User, entry.Compute.Host)
			return syscall.Exec(sshBin, []string{"ssh", "-t", target, remoteCmd}, os.Environ())
		},
	}
}

func newServiceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <env>",
		Short: "List services and their status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]
			ca, _, err := resolveRemoteEnv(cmd, envName)
			if err != nil {
				return err
			}

			Header(fmt.Sprintf("Services in %s", styleDetail.Render(envName)))

			composeCmd := "docker compose -f .previewctl.compose.yaml ps --format '{{.Name}}\t{{.State}}\t{{.Status}}'"
			out, err := ca.Exec(cmd.Context(), composeCmd, nil)
			if err != nil {
				return fmt.Errorf("listing services: %w", err)
			}

			if strings.TrimSpace(out) == "" {
				fmt.Fprintln(os.Stderr, "  No services running")
			} else {
				for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
					fmt.Fprintf(os.Stderr, "  %s\n", line)
				}
			}
			fmt.Println()

			return nil
		},
	}
}

// resolveRemoteEnv validates remote mode, loads the environment and config,
// and returns SSH compute access.
func resolveRemoteEnv(cmd *cobra.Command, envName string) (domain.ComputeAccess, *domain.ProjectConfig, error) {
	if globalMode != "remote" {
		return nil, nil, fmt.Errorf("service commands are only available in remote mode (use -m remote)")
	}

	mgr, cfg, err := buildManager(nil)
	if err != nil {
		return nil, nil, err
	}

	entry, err := mgr.GetEnvironment(cmd.Context(), envName)
	if err != nil {
		return nil, nil, fmt.Errorf("loading environment: %w", err)
	}
	if entry == nil {
		return nil, nil, fmt.Errorf("environment '%s' not found", envName)
	}
	if entry.Compute == nil || entry.Compute.Type != "ssh" {
		return nil, nil, fmt.Errorf("environment '%s' is not a remote environment", envName)
	}

	ca := domain.NewDomainSSHComputeAccess(entry.Compute.Host, entry.Compute.User, entry.Compute.Path)
	return ca, cfg, nil
}

// getRemoteEntry loads a remote environment entry.
func getRemoteEntry(cmd *cobra.Command, envName string) (*domain.EnvironmentEntry, error) {
	mgr, _, err := buildManager(nil)
	if err != nil {
		return nil, err
	}
	entry, err := mgr.GetEnvironment(cmd.Context(), envName)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, fmt.Errorf("environment '%s' not found", envName)
	}
	return entry, nil
}
