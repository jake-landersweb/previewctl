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
preview environment. Requires --mode remote and --env.`,
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
		Use:   "start <service>",
		Short: "Build and start a service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svcName := args[0]
			ca, cfg, err := resolveRemoteEnv(cmd)
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

			// Track as enabled
			if err := trackServiceEnabled(cmd, svcName); err != nil {
				return err
			}

			Success(fmt.Sprintf("Service %s started", styleDetail.Render(svcName)))
			return nil
		},
	}
}

func newServiceStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <service>",
		Short: "Stop a running service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svcName := args[0]
			ca, _, err := resolveRemoteEnv(cmd)
			if err != nil {
				return err
			}

			Header(fmt.Sprintf("Stopping %s", styleDetail.Render(svcName)))
			composeCmd := fmt.Sprintf("docker compose -f .previewctl.compose.yaml stop %s", svcName)
			if _, err := ca.Exec(cmd.Context(), composeCmd, nil); err != nil {
				return fmt.Errorf("stopping service: %w", err)
			}

			// Track as disabled
			if err := trackServiceDisabled(cmd, svcName); err != nil {
				return err
			}

			Success(fmt.Sprintf("Service %s stopped", styleDetail.Render(svcName)))
			return nil
		},
	}
}

func newServiceRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart <service>",
		Short: "Rebuild and restart a service",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svcName := args[0]
			ca, cfg, err := resolveRemoteEnv(cmd)
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
		Use:   "logs [service]",
		Short: "Stream service logs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ca, _, err := resolveRemoteEnv(cmd)
			if err != nil {
				return err
			}

			svcArg := ""
			if len(args) > 0 {
				svcArg = args[0]
			}

			sshCA, ok := ca.(*domain.DomainSSHComputeAccess)
			if !ok {
				return fmt.Errorf("environment does not support SSH")
			}

			composeCmd := fmt.Sprintf("docker compose -f .previewctl.compose.yaml logs -f %s", svcArg)
			remoteCmd := fmt.Sprintf("cd %q && %s", ca.Root(), composeCmd)

			sshBin, err := exec.LookPath("ssh")
			if err != nil {
				return fmt.Errorf("ssh not found: %w", err)
			}

			sshArgs := append([]string{"ssh", "-t"}, sshCA.SSHArgs()...)
			sshArgs = append(sshArgs, remoteCmd)
			return syscall.Exec(sshBin, sshArgs, os.Environ())
		},
	}
}

func newServiceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List services and their status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := globalEnvName
			ca, _, err := resolveRemoteEnv(cmd)
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
func resolveRemoteEnv(cmd *cobra.Command) (domain.ComputeAccess, *domain.ProjectConfig, error) {
	envName := globalEnvName
	if envName == "" {
		return nil, nil, fmt.Errorf("--env (-e) is required for service commands")
	}

	if resolvedMode() != "remote" {
		return nil, nil, fmt.Errorf("service commands are only available for remote environments")
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

	ca := mgr.BuildSSHComputeAccess(entry)
	return ca, cfg, nil
}

// trackServiceEnabled adds a service to the environment's enabled set and persists.
func trackServiceEnabled(cmd *cobra.Command, svcName string) error {
	envName := globalEnvName
	mgr, _, err := buildManager(nil)
	if err != nil {
		return err
	}
	entry, err := mgr.GetEnvironment(cmd.Context(), envName)
	if err != nil || entry == nil {
		return nil // best-effort
	}
	entry.EnableService(svcName)
	return mgr.SaveEnvironment(cmd.Context(), envName, entry)
}

// trackServiceDisabled removes a service from the environment's enabled set and persists.
func trackServiceDisabled(cmd *cobra.Command, svcName string) error {
	envName := globalEnvName
	mgr, _, err := buildManager(nil)
	if err != nil {
		return err
	}
	entry, err := mgr.GetEnvironment(cmd.Context(), envName)
	if err != nil || entry == nil {
		return nil // best-effort
	}
	entry.DisableService(svcName)
	return mgr.SaveEnvironment(cmd.Context(), envName, entry)
}

