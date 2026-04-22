package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jake-landersweb/previewctl/src/domain"
	"github.com/spf13/cobra"
)

func newCoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "core",
		Short: "Manage core services (e.g., postgres, redis)",
		Long:  "Manage core services defined in your previewctl.yaml. Run 'previewctl core <name> --help' to see available actions.",
	}

	if cfg, _, err := loadConfig(); err == nil {
		addCoreServiceCommands(cmd, cfg)
	}

	return cmd
}

func addCoreServiceCommands(parent *cobra.Command, cfg *domain.ProjectConfig) {
	for name, svc := range cfg.Provisioner.Services {
		svcCmd := newCoreServiceCmd(name, svc)
		parent.AddCommand(svcCmd)
	}
}

func newCoreServiceCmd(name string, svc domain.ProvisionerServiceConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("Manage core service: %s", name),
	}

	if svc.Init != "" {
		cmd.AddCommand(newCoreInitCmd(name))
	}
	if svc.Seed != "" {
		cmd.AddCommand(newCoreSeedCmd(name))
	}
	if svc.Reset != "" {
		cmd.AddCommand(newCoreResetCmd(name))
	}
	if svc.Destroy != "" {
		cmd.AddCommand(newCoreDestroyCmd(name))
	}

	return cmd
}

func newCoreInitCmd(svcName string) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Run one-time initialization",
		RunE: func(cmd *cobra.Command, args []string) error {
			progress := NewCLIProgressReporter()
			mgr, _, err := buildManager(progress)
			if err != nil {
				return err
			}

			Header(fmt.Sprintf("Initializing %s", styleDetail.Render(svcName)))

			if err := mgr.CoreInit(cmd.Context(), svcName); err != nil {
				return err
			}

			Success(fmt.Sprintf("Service %s initialized", svcName))
			return nil
		},
	}
}

func newCoreSeedCmd(svcName string) *cobra.Command {
	return &cobra.Command{
		Use:   "seed",
		Short: "Run the seed hook for a specific environment",
		Args:  cobra.NoArgs,
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

			Header(fmt.Sprintf("Seeding %s for %s",
				styleDetail.Render(svcName),
				styleDetail.Render(envName)))

			outputs, err := mgr.RunCoreHook(cmd.Context(), svcName, "seed", envName)
			if err != nil {
				return err
			}

			for k, v := range outputs {
				DetailKeyValue(k, v)
			}

			Success(fmt.Sprintf("Service %s seeded for %s", svcName, envName))
			return nil
		},
	}
}

func newCoreResetCmd(svcName string) *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Reset this service for a specific environment",
		Args:  cobra.NoArgs,
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

			Header(fmt.Sprintf("Resetting %s for %s",
				styleDetail.Render(svcName),
				styleDetail.Render(envName)))

			if err := mgr.CoreReset(cmd.Context(), svcName, envName); err != nil {
				return err
			}

			Success(fmt.Sprintf("Service %s reset for %s", svcName, envName))
			return nil
		},
	}
}

func newCoreDestroyCmd(svcName string) *cobra.Command {
	return &cobra.Command{
		Use:   "destroy",
		Short: "Destroy this service for a specific environment",
		Args:  cobra.NoArgs,
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

			Header(fmt.Sprintf("Destroying %s for %s",
				styleDetail.Render(svcName),
				styleDetail.Render(envName)))

			outputs, err := mgr.RunCoreHook(cmd.Context(), svcName, "destroy", envName)
			if err != nil {
				return err
			}
			_ = outputs

			Success(fmt.Sprintf("Service %s destroyed for %s", svcName, envName))
			return nil
		},
	}
}
