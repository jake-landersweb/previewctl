package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newInfraCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infra",
		Short: "Manage infrastructure containers (e.g., redis, postgres)",
		Long: `Start, stop, restart, and view logs for infrastructure containers
defined in your infrastructure compose file.`,
	}

	cmd.AddCommand(
		newInfraStartCmd(),
		newInfraStopCmd(),
		newInfraRestartCmd(),
		newInfraLogsCmd(),
	)

	return cmd
}

// resolveInfraCompose loads the config and returns the absolute compose file path
// and an environment slice with COMPOSE_PROJECT_NAME set.
func resolveInfraCompose() (composeFile string, env []string, err error) {
	cfg, projectRoot, err := loadConfigWithMode(resolvedMode())
	if err != nil {
		return "", nil, err
	}

	if cfg.Infrastructure == nil || cfg.Infrastructure.ComposeFile == "" {
		return "", nil, fmt.Errorf("no infrastructure compose file configured in previewctl.yaml")
	}

	composeFile = filepath.Join(projectRoot, cfg.Infrastructure.ComposeFile)
	if _, err := os.Stat(composeFile); err != nil {
		return "", nil, fmt.Errorf("infrastructure compose file not found: %s", composeFile)
	}

	envName := globalEnvName
	if envName == "" {
		envName = "default"
	}

	env = append(os.Environ(),
		fmt.Sprintf("COMPOSE_PROJECT_NAME=previewctl-%s-%s", cfg.Name, envName),
	)

	return composeFile, env, nil
}

func newInfraStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start [service...]",
		Short: "Start infrastructure containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			composeFile, env, err := resolveInfraCompose()
			if err != nil {
				return err
			}

			composeArgs := []string{"compose", "-f", composeFile, "up", "-d"}
			composeArgs = append(composeArgs, args...)

			c := exec.CommandContext(cmd.Context(), "docker", composeArgs...)
			c.Env = env
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return fmt.Errorf("starting infrastructure: %w", err)
			}

			Success("Infrastructure started")
			return nil
		},
	}
}

func newInfraStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [service...]",
		Short: "Stop infrastructure containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			composeFile, env, err := resolveInfraCompose()
			if err != nil {
				return err
			}

			composeArgs := []string{"compose", "-f", composeFile, "stop"}
			composeArgs = append(composeArgs, args...)

			c := exec.CommandContext(cmd.Context(), "docker", composeArgs...)
			c.Env = env
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return fmt.Errorf("stopping infrastructure: %w", err)
			}

			Success("Infrastructure stopped")
			return nil
		},
	}
}

func newInfraRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart [service...]",
		Short: "Restart infrastructure containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			composeFile, env, err := resolveInfraCompose()
			if err != nil {
				return err
			}

			composeArgs := []string{"compose", "-f", composeFile, "restart"}
			composeArgs = append(composeArgs, args...)

			c := exec.CommandContext(cmd.Context(), "docker", composeArgs...)
			c.Env = env
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return fmt.Errorf("restarting infrastructure: %w", err)
			}

			Success("Infrastructure restarted")
			return nil
		},
	}
}

func newInfraLogsCmd() *cobra.Command {
	var (
		follow     bool
		tail       string
		since      string
		until      string
		timestamps bool
		noColor    bool
	)

	cmd := &cobra.Command{
		Use:   "logs [service...]",
		Short: "View infrastructure container logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			composeFile, env, err := resolveInfraCompose()
			if err != nil {
				return err
			}

			composeArgs := []string{"compose", "-f", composeFile, "logs"}
			if follow {
				composeArgs = append(composeArgs, "-f")
			}
			if tail != "" {
				composeArgs = append(composeArgs, "--tail", tail)
			}
			if since != "" {
				composeArgs = append(composeArgs, "--since", since)
			}
			if until != "" {
				composeArgs = append(composeArgs, "--until", until)
			}
			if timestamps {
				composeArgs = append(composeArgs, "-t")
			}
			if noColor {
				composeArgs = append(composeArgs, "--no-color")
			}
			composeArgs = append(composeArgs, args...)

			c := exec.CommandContext(cmd.Context(), "docker", composeArgs...)
			c.Env = env
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return fmt.Errorf("fetching logs: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().StringVar(&tail, "tail", "", "Number of lines to show from the end (e.g., 50)")
	cmd.Flags().StringVar(&since, "since", "", "Show logs since timestamp (e.g., 30m, 1h)")
	cmd.Flags().StringVar(&until, "until", "", "Show logs until timestamp")
	cmd.Flags().BoolVarP(&timestamps, "timestamps", "t", false, "Show timestamps")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "Disable colored output")

	return cmd
}
