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
		Short: "Manage core services",
		Long:  "Manage core services defined in your previewctl.yaml. Run 'previewctl core <name> --help' to see available actions.",
	}

	if cfg, _, err := loadConfig(); err == nil {
		addCoreServiceCommands(cmd, cfg)
	}

	return cmd
}

func addCoreServiceCommands(parent *cobra.Command, cfg *domain.ProjectConfig) {
	for name, svc := range cfg.Core.Services {
		svcCmd := newCoreServiceCmd(name, svc)
		parent.AddCommand(svcCmd)
	}
}

func newCoreServiceCmd(name string, svc domain.CoreServiceConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   name,
		Short: fmt.Sprintf("Manage core service: %s", name),
	}

	// Only add commands for hooks that are defined
	if svc.Hooks != nil {
		if svc.Hooks.Init != "" {
			cmd.AddCommand(newCoreInitCmd(name))
		}
		if svc.Hooks.Seed != "" {
			cmd.AddCommand(newCoreSeedCmd(name))
		}
		if svc.Hooks.Reset != "" {
			cmd.AddCommand(newCoreResetCmd(name))
		}
		if svc.Hooks.Destroy != "" {
			cmd.AddCommand(newCoreDestroyCmd(name))
		}
	}

	return cmd
}

func newCoreInitCmd(svcName string) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Run one-time initialization for this core service",
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

			Success(fmt.Sprintf("Core service %s initialized", svcName))
			return nil
		},
	}
}

func newCoreSeedCmd(svcName string) *cobra.Command {
	return &cobra.Command{
		Use:   "seed [env]",
		Short: "Run the seed hook for a specific environment",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			progress := NewCLIProgressReporter()
			mgr, cfg, err := buildManager(progress)
			if err != nil {
				return err
			}

			home, _ := os.UserHomeDir()
			statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")

			envName, err := resolveEnvName(args, statePath)
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

			Success(fmt.Sprintf("Core service %s seeded for %s", svcName, envName))
			return nil
		},
	}
}

func newCoreResetCmd(svcName string) *cobra.Command {
	return &cobra.Command{
		Use:   "reset [env]",
		Short: "Reset this core service for a specific environment",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			progress := NewCLIProgressReporter()
			mgr, cfg, err := buildManager(progress)
			if err != nil {
				return err
			}

			home, _ := os.UserHomeDir()
			statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")

			envName, err := resolveEnvName(args, statePath)
			if err != nil {
				return fmt.Errorf("could not determine environment: %w", err)
			}

			Header(fmt.Sprintf("Resetting %s for %s",
				styleDetail.Render(svcName),
				styleDetail.Render(envName)))

			if err := mgr.CoreReset(cmd.Context(), svcName, envName); err != nil {
				return err
			}

			Success(fmt.Sprintf("Core service %s reset for %s", svcName, envName))
			return nil
		},
	}
}

func newCoreDestroyCmd(svcName string) *cobra.Command {
	return &cobra.Command{
		Use:   "destroy [env]",
		Short: "Destroy this core service for a specific environment",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			progress := NewCLIProgressReporter()
			mgr, cfg, err := buildManager(progress)
			if err != nil {
				return err
			}

			home, _ := os.UserHomeDir()
			statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")

			envName, err := resolveEnvName(args, statePath)
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

			Success(fmt.Sprintf("Core service %s destroyed for %s", svcName, envName))
			return nil
		},
	}
}
