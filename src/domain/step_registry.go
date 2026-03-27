package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// stepRegistry builds StepOpts for runner-phase steps.
// It captures the shared context needed by all step closures.
type stepRegistry struct {
	m        *Manager
	entry    *EnvironmentEntry
	ca       ComputeAccess
	manifest *Manifest
	envName  string
	branch   string
}

// newStepRegistry creates a registry for building runner step options.
func newStepRegistry(m *Manager, entry *EnvironmentEntry, ca ComputeAccess, manifest *Manifest, envName, branch string) *stepRegistry {
	return &stepRegistry{m: m, entry: entry, ca: ca, manifest: manifest, envName: envName, branch: branch}
}

// get returns the StepOpts for a named runner step, or an error if unknown.
// ctx is captured into the returned closures.
func (r *stepRegistry) get(ctx context.Context, name string) (StepOpts, error) {
	switch name {
	case "sync_code":
		return r.syncCode(ctx), nil
	case "generate_manifest":
		return r.generateManifest(ctx), nil
	case "runner_before":
		return r.runnerBefore(ctx), nil
	case "generate_env":
		return r.generateEnv(ctx), nil
	case "start_infra":
		return r.startInfra(ctx), nil
	case "generate_compose":
		return r.generateCompose(ctx), nil
	case "generate_nginx":
		return r.generateNginx(ctx), nil
	case "build_services":
		return r.buildServices(ctx), nil
	case "start_services":
		return r.startServices(ctx), nil
	case "runner_deploy":
		return r.runnerDeploy(ctx), nil
	case "runner_after":
		return r.runnerAfter(ctx), nil
	default:
		return StepOpts{}, fmt.Errorf("unknown runner step '%s'", name)
	}
}

func (r *stepRegistry) syncCode(ctx context.Context) StepOpts {
	branch := r.branch
	return StepOpts{
		Name:        "sync_code",
		StartMsg:    fmt.Sprintf("Syncing code to origin/%s...", branch),
		CompleteMsg: msg("Code synced to latest"),
		Fn: func() error {
			syncCmd := fmt.Sprintf("git fetch --depth 1 origin %s && git reset --hard origin/%s", branch, branch)
			_, err := r.ca.Exec(ctx, syncCmd, nil)
			return err
		},
	}
}

func (r *stepRegistry) generateManifest(ctx context.Context) StepOpts {
	cfg := r.m.config
	entry := r.entry
	return StepOpts{
		Name:        "generate_manifest",
		StartMsg:    "Regenerating manifest...",
		CompleteMsg: msg("Manifest regenerated"),
		Fn: func() error {
			mode := cfg.Mode
			if mode == "" {
				mode = "local"
			}
			manifest, err := BuildManifest(cfg, r.envName, r.branch, mode, entry.Ports, entry.ProvisionerOutputs, entry.Env)
			if err != nil {
				return fmt.Errorf("building manifest: %w", err)
			}
			// Preserve enabled services from the entry
			manifest.EnabledServices = entry.EnabledServices

			data, err := json.MarshalIndent(manifest, "", "  ")
			if err != nil {
				return err
			}
			if err := r.ca.WriteFile(ctx, ".previewctl.json", data, 0o644); err != nil {
				return err
			}
			// Update the registry's manifest so subsequent steps use the fresh version
			*r.manifest = *manifest
			return nil
		},
	}
}

func (r *stepRegistry) runnerBefore(ctx context.Context) StepOpts {
	cfg := r.m.config
	if cfg.Runner == nil || cfg.Runner.Before == "" {
		return StepOpts{
			Name:        "runner_before",
			StartMsg:    "runner.before (not configured)",
			CompleteMsg: msg("runner.before skipped (not configured)"),
			Fn:          func() error { return nil },
		}
	}
	beforeMsg := fmt.Sprintf("Ran runner.before (%s)", cfg.Runner.Before)
	return StepOpts{
		Name:        "runner_before",
		StartMsg:    fmt.Sprintf("Running runner.before → %s", cfg.Runner.Before),
		CompleteMsg: &beforeMsg,
		Fn: func() error {
			r.m.progress.OnStep(StepEvent{Step: "runner_before", Status: StepStreaming, Message: fmt.Sprintf("Running runner.before → %s", cfg.Runner.Before)})
			env := r.m.buildHookEnv(r.envName, r.ca.Root(), r.manifest.Ports, r.entry.Env)
			_, err := r.ca.Exec(ctx, cfg.Runner.Before, env)
			return err
		},
	}
}

func (r *stepRegistry) generateEnv(ctx context.Context) StepOpts {
	envFiles := r.manifest.EnvFilePaths()
	return StepOpts{
		Name:        "generate_env",
		StartMsg:    "Generating .env files...",
		CompleteMsg: msg(".env files generated"),
		Fn: func() error {
			if len(envFiles) == 0 {
				return nil
			}
			var script strings.Builder
			script.WriteString("set -e\n")
			for relPath, envVars := range envFiles {
				content := RenderEnvFileContent(envVars)
				dir := filepath.Dir(relPath)
				if dir != "." {
					fmt.Fprintf(&script, "mkdir -p %q\n", dir)
				}
				fmt.Fprintf(&script, "cat > %q <<'ENVEOF'\n%sENVEOF\n", relPath, string(content))
			}
			_, err := r.ca.Exec(ctx, script.String(), nil)
			return err
		},
		Verify: func(ctx context.Context) error {
			for relPath := range envFiles {
				if _, err := r.ca.ReadFile(ctx, relPath); err != nil {
					return fmt.Errorf("env file %s missing: %w", relPath, err)
				}
				return nil // just check the first one
			}
			return nil
		},
	}
}

func (r *stepRegistry) startInfra(ctx context.Context) StepOpts {
	cfg := r.m.config
	manifest := r.manifest
	return StepOpts{
		Name:        "start_infra",
		StartMsg:    "Starting infrastructure containers...",
		CompleteMsg: msg("Infrastructure containers started"),
		Fn: func() error {
			composeFile := ""
			if manifest.Infrastructure != nil {
				composeFile = manifest.Infrastructure.ComposeFile
			}
			if composeFile == "" {
				return nil
			}
			env := BuildComposeEnv(cfg.Name, r.envName, manifest.Ports)
			cmd := fmt.Sprintf("docker compose -f %s up -d", composeFile)
			_, err := r.ca.Exec(ctx, cmd, env)
			return err
		},
		Verify: func(ctx context.Context) error {
			composeFile := ""
			if manifest.Infrastructure != nil {
				composeFile = manifest.Infrastructure.ComposeFile
			}
			if composeFile == "" {
				return nil
			}
			projectName := ComposeProjectName(cfg.Name, r.envName)
			cmd := fmt.Sprintf("docker compose -f %s -p %s ps --format json", composeFile, projectName)
			out, err := r.ca.Exec(ctx, cmd, nil)
			if err != nil {
				return err
			}
			if len(strings.TrimSpace(out)) == 0 {
				return fmt.Errorf("infrastructure not running")
			}
			return nil
		},
	}
}

func (r *stepRegistry) generateCompose(ctx context.Context) StepOpts {
	cfg := r.m.config
	manifest := r.manifest
	return StepOpts{
		Name:        "generate_compose",
		StartMsg:    "Generating Docker Compose file...",
		CompleteMsg: msg("Docker Compose file generated"),
		Fn: func() error {
			data, err := GenerateComposeFile(cfg, manifest)
			if err != nil {
				return fmt.Errorf("generating compose file: %w", err)
			}
			return r.ca.WriteFile(ctx, ".previewctl.compose.yaml", data, 0o644)
		},
		Verify: func(ctx context.Context) error {
			if _, err := r.ca.ReadFile(ctx, ".previewctl.compose.yaml"); err != nil {
				return fmt.Errorf("compose file missing: %w", err)
			}
			return nil
		},
	}
}

func (r *stepRegistry) generateNginx(ctx context.Context) StepOpts {
	cfg := r.m.config
	manifest := r.manifest
	return StepOpts{
		Name:        "generate_nginx",
		StartMsg:    "Generating nginx config and error pages...",
		CompleteMsg: msg("nginx config generated"),
		Fn: func() error {
			if cfg.Runner == nil || cfg.Runner.Compose == nil || !cfg.Runner.Compose.Proxy.IsEnabled() {
				return nil
			}
			domain := cfg.Runner.Compose.Proxy.Domain
			if domain == "" {
				return fmt.Errorf("runner.compose.proxy.domain is required")
			}

			// Pass enabled services to manifest for nginx generation
			manifest.EnabledServices = r.enabledServices()

			data, err := GenerateNginxConfig(cfg, manifest, domain)
			if err != nil {
				return fmt.Errorf("generating nginx config: %w", err)
			}
			if err := r.ca.WriteFile(ctx, "preview/nginx.conf", data, 0o644); err != nil {
				return err
			}

			// Write error pages
			for filename, content := range GenerateErrorPages(manifest.EnvName) {
				if err := r.ca.WriteFile(ctx, "preview/error-pages/"+filename, content, 0o644); err != nil {
					return fmt.Errorf("writing error page %s: %w", filename, err)
				}
			}
			return nil
		},
		Verify: func(ctx context.Context) error {
			if _, err := r.ca.ReadFile(ctx, "preview/nginx.conf"); err != nil {
				return fmt.Errorf("nginx config missing: %w", err)
			}
			return nil
		},
	}
}

func (r *stepRegistry) buildServices(ctx context.Context) StepOpts {
	cfg := r.m.config
	return StepOpts{
		Name:        "build_services",
		StartMsg:    "Building services...",
		CompleteMsg: msg("Services built"),
		Fn: func() error {
			if cfg.Runner == nil || cfg.Runner.Compose == nil {
				return nil
			}
			r.m.progress.OnStep(StepEvent{Step: "build_services", Status: StepStreaming})
			services := r.enabledServices()
			var cmds []string
			var names []string
			for _, svcName := range services {
				svc, ok := cfg.Services[svcName]
				if !ok || svc.Build == "" {
					continue
				}
				cmds = append(cmds, svc.Build)
				names = append(names, svcName)
			}
			if len(cmds) == 0 {
				return nil
			}
			stderr := r.m.progress.StderrWriter()
			fmt.Fprintf(stderr, "    Services: %s\n", strings.Join(names, ", "))
			for i, cmd := range cmds {
				fmt.Fprintf(stderr, "    [%d/%d] %s\n", i+1, len(cmds), cmd)
			}
			_, err := r.ca.Exec(ctx, strings.Join(cmds, " && "), nil)
			return err
		},
	}
}

func (r *stepRegistry) startServices(ctx context.Context) StepOpts {
	cfg := r.m.config
	return StepOpts{
		Name:        "start_services",
		StartMsg:    "Starting services...",
		CompleteMsg: msg("Services started"),
		Fn: func() error {
			if cfg.Runner == nil || cfg.Runner.Compose == nil {
				return nil
			}
			// Seed EnabledServices from autostart on first run
			services := r.enabledServices()
			if r.entry != nil && len(r.entry.EnabledServices) == 0 {
				r.entry.EnabledServices = append([]string{}, services...)
			}

			var composeServices []string
			if cfg.Runner.Compose.Proxy.IsEnabled() {
				proxyType := cfg.Runner.Compose.Proxy.ResolvedType()
				composeServices = append(composeServices, proxyType)
			}
			composeServices = append(composeServices, services...)
			cmd := fmt.Sprintf("docker compose -f .previewctl.compose.yaml up -d %s", strings.Join(composeServices, " "))
			_, err := r.ca.Exec(ctx, cmd, nil)
			return err
		},
		Verify: func(ctx context.Context) error {
			cmd := "docker compose -f .previewctl.compose.yaml ps --format json"
			out, err := r.ca.Exec(ctx, cmd, nil)
			if err != nil {
				return err
			}
			if len(strings.TrimSpace(out)) == 0 {
				return fmt.Errorf("no services running")
			}
			return nil
		},
	}
}

// enabledServices returns the list of services to build/start.
// Uses entry.EnabledServices if populated, falls back to config autostart.
func (r *stepRegistry) enabledServices() []string {
	if r.entry != nil && len(r.entry.EnabledServices) > 0 {
		return r.entry.EnabledServices
	}
	cfg := r.m.config
	if cfg.Runner != nil && cfg.Runner.Compose != nil {
		return cfg.Runner.Compose.Autostart
	}
	return nil
}

func (r *stepRegistry) runnerDeploy(ctx context.Context) StepOpts {
	cfg := r.m.config
	if cfg.Runner == nil || cfg.Runner.Deploy == "" {
		return StepOpts{
			Name:        "runner_deploy",
			StartMsg:    "runner.deploy (not configured)",
			CompleteMsg: msg("runner.deploy skipped (not configured)"),
			Fn:          func() error { return nil },
		}
	}
	deployMsg := fmt.Sprintf("Ran runner.deploy (%s)", cfg.Runner.Deploy)
	return StepOpts{
		Name:        "runner_deploy",
		StartMsg:    fmt.Sprintf("Running runner.deploy → %s", cfg.Runner.Deploy),
		CompleteMsg: &deployMsg,
		Fn: func() error {
			r.m.progress.OnStep(StepEvent{Step: "runner_deploy", Status: StepStreaming, Message: fmt.Sprintf("Running runner.deploy → %s", cfg.Runner.Deploy)})
			env := r.m.buildHookEnv(r.envName, r.ca.Root(), r.manifest.Ports, r.entry.Env)
			_, err := r.ca.Exec(ctx, cfg.Runner.Deploy, env)
			return err
		},
	}
}

func (r *stepRegistry) runnerAfter(ctx context.Context) StepOpts {
	cfg := r.m.config
	if cfg.Runner == nil || cfg.Runner.After == "" {
		return StepOpts{
			Name:        "runner_after",
			StartMsg:    "runner.after (not configured)",
			CompleteMsg: msg("runner.after skipped (not configured)"),
			Fn:          func() error { return nil },
		}
	}
	afterMsg := fmt.Sprintf("Ran runner.after (%s)", cfg.Runner.After)
	return StepOpts{
		Name:        "runner_after",
		StartMsg:    fmt.Sprintf("Running runner.after → %s", cfg.Runner.After),
		CompleteMsg: &afterMsg,
		Fn: func() error {
			r.m.progress.OnStep(StepEvent{Step: "runner_after", Status: StepStreaming, Message: fmt.Sprintf("Running runner.after → %s", cfg.Runner.After)})
			env := r.m.buildHookEnv(r.envName, r.ca.Root(), r.manifest.Ports, r.entry.Env)
			_, err := r.ca.Exec(ctx, cfg.Runner.After, env)
			return err
		},
	}
}
