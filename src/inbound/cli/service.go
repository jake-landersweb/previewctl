package cli

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
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
			ca, cfg, entry, err := resolveRemoteEnvWithEntry(cmd)
			if err != nil {
				return err
			}

			svc, ok := cfg.Services[svcName]
			if !ok {
				return fmt.Errorf("unknown service '%s'", svcName)
			}

			if entry.IsServiceEnabled(svcName) {
				fmt.Fprintf(os.Stderr, "  %s Service %s is already running\n",
					styleDim.Render("·"),
					styleDetail.Render(svcName))
				return nil
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

			// Regenerate nginx config and reload so the 503 block becomes a proxy block
			if err := refreshNginxProxy(cmd); err != nil {
				fmt.Fprintf(os.Stderr, "  %s Warning: nginx refresh failed: %v\n",
					styleDim.Render("·"), err)
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
			ca, _, entry, err := resolveRemoteEnvWithEntry(cmd)
			if err != nil {
				return err
			}

			if !entry.IsServiceEnabled(svcName) {
				fmt.Fprintf(os.Stderr, "  %s Service %s is not running\n",
					styleDim.Render("·"),
					styleDetail.Render(svcName))
				return nil
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

			// Regenerate nginx config and reload so the proxy block becomes a 503 block
			if err := refreshNginxProxy(cmd); err != nil {
				fmt.Fprintf(os.Stderr, "  %s Warning: nginx refresh failed: %v\n",
					styleDim.Render("·"), err)
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

// optionalInt is a pflag.Value that supports --flag (uses default) and --flag N (uses N).
type optionalInt struct {
	val    int
	defVal int
	set    bool
}

func (o *optionalInt) String() string {
	if !o.set {
		return ""
	}
	return fmt.Sprintf("%d", o.val)
}

func (o *optionalInt) Set(s string) error {
	n, err := fmt.Sscanf(s, "%d", &o.val)
	if err != nil || n != 1 {
		return fmt.Errorf("must be a positive integer, got %q", s)
	}
	if o.val <= 0 {
		return fmt.Errorf("must be a positive integer, got %d", o.val)
	}
	o.set = true
	return nil
}

func (o *optionalInt) Type() string { return "int" }

// rewriteOptionalIntArgs rewrites os.Args so that "--flag N" becomes "--flag=N"
// for flags that use NoOptDefVal. This allows cobra to correctly parse
// optional-value flags that accept a space-separated integer.
// Must be called before cobra parses args (e.g., in the parent command setup).
func rewriteOptionalIntArgs(flags ...string) {
	flagSet := make(map[string]bool, len(flags))
	for _, f := range flags {
		flagSet["--"+f] = true
	}
	rewritten := make([]string, 0, len(os.Args))
	for i := 0; i < len(os.Args); i++ {
		arg := os.Args[i]
		if flagSet[arg] && i+1 < len(os.Args) {
			next := os.Args[i+1]
			// If the next arg looks like a number, merge them
			if len(next) > 0 && next[0] >= '0' && next[0] <= '9' {
				rewritten = append(rewritten, arg+"="+next)
				i++ // skip next
				continue
			}
		}
		rewritten = append(rewritten, arg)
	}
	os.Args = rewritten
}

func newServiceLogsCmd() *cobra.Command {
	// Rewrite os.Args before cobra parses so "--tail 100" becomes "--tail=100".
	// This is necessary because NoOptDefVal flags don't consume the next token.
	rewriteOptionalIntArgs("head", "tail")

	var (
		follow     bool
		head       = &optionalInt{defVal: 50}
		tail       = &optionalInt{defVal: 50}
		since      string
		until      string
		timestamps bool
		noColor    bool
	)

	cmd := &cobra.Command{
		Use:   "logs [service]",
		Short: "Stream service logs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ca, cfg, entry, err := resolveRemoteEnvWithEntry(cmd)
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

			if head.set && tail.set {
				return fmt.Errorf("--head and --tail are mutually exclusive")
			}

			// Build docker compose logs command with flags
			projectName := domain.ComposeProjectName(cfg.Name, entry.Name)
			composeArgs := fmt.Sprintf("docker compose -f .previewctl.compose.yaml -p %s logs", projectName)
			if follow {
				composeArgs += " -f"
			}
			if tail.set {
				composeArgs += fmt.Sprintf(" --tail %d", tail.val)
			}
			if since != "" {
				composeArgs += fmt.Sprintf(" --since %s", since)
			}
			if until != "" {
				composeArgs += fmt.Sprintf(" --until %s", until)
			}
			if timestamps {
				composeArgs += " -t"
			}
			if noColor {
				composeArgs += " --no-color"
			}
			if svcArg != "" {
				composeArgs += " " + svcArg
			}

			var remoteCmd string
			if head.set {
				remoteCmd = fmt.Sprintf("cd %q && %s | head -n %d", ca.Root(), composeArgs, head.val)
			} else {
				remoteCmd = fmt.Sprintf("cd %q && %s", ca.Root(), composeArgs)
			}

			sshBin, err := exec.LookPath("ssh")
			if err != nil {
				return fmt.Errorf("ssh not found: %w", err)
			}

			sshArgs := append([]string{"ssh", "-t"}, sshCA.SSHArgs()...)
			sshArgs = append(sshArgs, remoteCmd)
			return syscall.Exec(sshBin, sshArgs, os.Environ())
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().VarP(head, "head", "", "Number of lines to show from the beginning (default 50)")
	cmd.Flags().VarP(tail, "tail", "", "Number of lines to show from the end (default 50)")
	cmd.Flags().Lookup("head").NoOptDefVal = "50"
	cmd.Flags().Lookup("tail").NoOptDefVal = "50"
	cmd.Flags().StringVar(&since, "since", "", "Show logs since timestamp (e.g., 2024-01-01T00:00:00, 30m, 1h)")
	cmd.Flags().StringVar(&until, "until", "", "Show logs until timestamp")
	cmd.Flags().BoolVarP(&timestamps, "timestamps", "t", false, "Show timestamps")
	cmd.Flags().BoolVar(&noColor, "no-color", false, "Disable colored output")

	return cmd
}

func newServiceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List services and their status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := globalEnvName
			ca, cfg, entry, err := resolveRemoteEnvWithEntry(cmd)
			if err != nil {
				return err
			}

			// Get running container state from docker compose
			composeCmd := "docker compose -f .previewctl.compose.yaml ps --format '{{.Service}}\t{{.State}}\t{{.Status}}'"
			out, err := ca.Exec(cmd.Context(), composeCmd, nil)
			if err != nil {
				return fmt.Errorf("listing services: %w", err)
			}

			// Parse docker state into a map
			running := make(map[string]string) // service name → status string
			for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
				parts := strings.SplitN(line, "\t", 3)
				if len(parts) >= 2 {
					running[parts[0]] = strings.Join(parts[1:], " ")
				}
			}

			// Resolve proxy domain for URLs
			var proxyDomain string
			if cfg.Runner != nil && cfg.Runner.Compose != nil && cfg.Runner.Compose.Proxy != nil {
				proxyDomain = cfg.Runner.Compose.Proxy.Domain
			}

			Header(fmt.Sprintf("Services in %s", styleDetail.Render(envName)))

			// Show all known services from config, sorted
			svcNames := make([]string, 0, len(cfg.Services))
			maxLen := 0
			for name := range cfg.Services {
				svcNames = append(svcNames, name)
				if len(name) > maxLen {
					maxLen = len(name)
				}
			}
			sort.Strings(svcNames)
			pad := fmt.Sprintf("%%-%ds", maxLen+2)

			for _, name := range svcNames {
				enabled := entry.IsServiceEnabled(name)
				dockerStatus, isRunning := running[name]

				// Status indicator
				var status string
				if isRunning {
					status = styleSuccess.Render("● running")
				} else if enabled {
					status = styleFail.Render("● stopped") + " " + styleDim.Render("(enabled)")
				} else {
					status = styleDim.Render("○ disabled")
				}

				// URL
				var url string
				if proxyDomain != "" {
					url = fmt.Sprintf("https://%s--%s.%s", envName, name, proxyDomain)
				}

				line := fmt.Sprintf("  %s  %s", styleMessage.Render(fmt.Sprintf(pad, name)), status)
				if isRunning && dockerStatus != "" {
					line += "  " + styleDim.Render(dockerStatus)
				}
				if url != "" && (isRunning || enabled) {
					line += "  " + styleDetail.Render(url)
				}
				fmt.Fprintln(os.Stderr, line)
			}
			fmt.Fprintln(os.Stderr)

			return nil
		},
	}
}

// resolveRemoteEnv validates remote mode, loads the environment and config,
// and returns SSH compute access.
func resolveRemoteEnv(cmd *cobra.Command) (domain.ComputeAccess, *domain.ProjectConfig, error) {
	ca, cfg, _, err := resolveRemoteEnvWithEntry(cmd)
	return ca, cfg, err
}

// resolveRemoteEnvWithEntry is like resolveRemoteEnv but also returns the entry.
func resolveRemoteEnvWithEntry(cmd *cobra.Command) (domain.ComputeAccess, *domain.ProjectConfig, *domain.EnvironmentEntry, error) {
	envName := globalEnvName
	if envName == "" {
		return nil, nil, nil, fmt.Errorf("--env (-e) is required for service commands")
	}

	if resolvedMode() != "remote" {
		return nil, nil, nil, fmt.Errorf("service commands are only available for remote environments")
	}

	mgr, cfg, err := buildManager(nil)
	if err != nil {
		return nil, nil, nil, err
	}

	entry, err := mgr.GetEnvironment(cmd.Context(), envName)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("loading environment: %w", err)
	}
	if entry == nil {
		return nil, nil, nil, fmt.Errorf("environment '%s' not found", envName)
	}
	if entry.Compute == nil || entry.Compute.Type != "ssh" {
		return nil, nil, nil, fmt.Errorf("environment '%s' is not a remote environment", envName)
	}

	ca := mgr.BuildSSHComputeAccess(entry)
	return ca, cfg, entry, nil
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

// refreshNginxProxy re-runs the generate_nginx step and restarts the nginx
// container so that service start/stop changes are reflected immediately.
func refreshNginxProxy(cmd *cobra.Command) error {
	envName := globalEnvName
	mgr, _, err := buildManager(nil)
	if err != nil {
		return err
	}

	// Re-run the generate_nginx step to regenerate the config
	Header("Regenerating nginx config")
	if err := mgr.RunStep(cmd.Context(), envName, "generate_nginx"); err != nil {
		return fmt.Errorf("regenerating nginx config: %w", err)
	}

	// Restart nginx container to pick up the new config
	Header("Restarting nginx proxy")
	ca, _, err := resolveRemoteEnv(cmd)
	if err != nil {
		return fmt.Errorf("resolving environment: %w", err)
	}
	if _, err := ca.Exec(cmd.Context(), "docker compose -f .previewctl.compose.yaml restart nginx", nil); err != nil {
		return fmt.Errorf("restarting nginx: %w", err)
	}

	return nil
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
