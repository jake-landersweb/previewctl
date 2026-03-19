package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/jake-landersweb/previewctl/src/domain"
	"github.com/spf13/cobra"
)

func newVetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vet",
		Short: "Vet the previewctl.yaml configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, projectRoot, err := loadConfig()
			if err != nil {
				return err
			}

			Header(fmt.Sprintf("Validating %s", styleDetail.Render(cfg.Name)))

			fileExists := func(path string) bool {
				_, err := os.Stat(path)
				return err == nil
			}

			err = domain.ValidateConfigWithFS(cfg, projectRoot, fileExists)
			if err != nil {
				ve, ok := err.(*domain.ValidationError)
				if ok {
					for _, e := range ve.Errors {
						fmt.Fprintf(os.Stderr, "  %s %s\n", styleFail.Render("✗"), styleFail.Render(e))
					}
					fmt.Fprintln(os.Stderr)
					return fmt.Errorf("%d validation error(s) found", len(ve.Errors))
				}
				return err
			}

			// Print summary of what was validated
			dbCount := len(cfg.Core.Databases)
			infraCount := len(cfg.InfraServices)
			svcCount := len(cfg.Services)

			KeyValue("Version", fmt.Sprintf("%d", cfg.Version))
			KeyValue("Services", fmt.Sprintf("%d", svcCount))
			KeyValue("Infrastructure", fmt.Sprintf("%d", infraCount))
			KeyValue("Databases", fmt.Sprintf("%d", dbCount))

			if cfg.Infrastructure != nil && cfg.Infrastructure.ComposeFile != "" {
				KeyValue("Compose file", cfg.Infrastructure.ComposeFile)
			}

			// Count total env vars and template refs
			envVarCount := 0
			for _, svc := range cfg.Services {
				envVarCount += len(svc.Env)
			}
			KeyValue("Env vars", fmt.Sprintf("%d across %d services", envVarCount, svcCount))

			// Check Docker
			dockerStatus := "running"
			if err := exec.Command("docker", "info").Run(); err != nil {
				dockerStatus = "not running"
			}
			KeyValue("Docker", dockerStatus)
			if dockerStatus == "not running" {
				fmt.Fprintf(os.Stderr, "  %s %s\n",
					styleSkipped.Render("⚠"),
					styleSkipped.Render("Docker daemon is not running — previewctl requires Docker for infrastructure"),
				)
			}

			// Show port allocation info
			serviceNames := cfg.ServiceNames()
			if len(serviceNames) > 0 {
				KeyValue("Port allocation", fmt.Sprintf("%d services, range 61000–65000", len(serviceNames)))
			}

			Success("Configuration is valid")

			return nil
		},
	}

	return cmd
}
