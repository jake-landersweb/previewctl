package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jake-landersweb/previewctl/src/domain"
	"github.com/spf13/cobra"
)

func newInfraCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infra",
		Short: "Manage infrastructure containers (e.g., redis, postgres)",
		Long: `Start, stop, restart, view logs, and inspect the status of infrastructure
containers defined in your infrastructure compose file. For remote environments
all operations are routed through the environment's compute host over SSH.`,
	}

	cmd.AddCommand(
		newInfraStartCmd(),
		newInfraStopCmd(),
		newInfraRestartCmd(),
		newInfraLogsCmd(),
		newInfraStatusCmd(),
	)

	return cmd
}

// infraTarget bundles everything needed to invoke a docker compose subcommand
// against the environment's infrastructure, whether that environment is backed
// by the local host or an SSH-accessible remote compute.
type infraTarget struct {
	ca          domain.ComputeAccess
	composeFile string // path to compose file, relative to ca.Root()
	projectName string
	services    map[string]domain.InfraService
	composeEnv  []string
	envName     string
	remote      bool
	host        string
}

// resolveInfraTarget loads config + state, determines whether the resolved
// environment lives locally or on a remote SSH host, and returns the compute
// access binding needed to run docker compose against it.
func resolveInfraTarget(ctx context.Context) (*infraTarget, error) {
	mgr, cfg, err := buildManager(nil)
	if err != nil {
		return nil, err
	}

	home, _ := os.UserHomeDir()
	statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")
	envName, err := requireEnv(statePath)
	if err != nil {
		return nil, fmt.Errorf("could not determine environment: %w", err)
	}

	entry, err := mgr.GetEnvironment(ctx, envName)
	if err != nil {
		return nil, fmt.Errorf("loading environment: %w", err)
	}

	return buildInfraTarget(mgr, cfg, entry, envName)
}

// buildInfraTarget constructs an infraTarget from an already-loaded environment
// entry. Callers that have already resolved the manager + entry (e.g. the root
// `status` command via mgr.Status) use this to avoid a second state lookup.
func buildInfraTarget(mgr *domain.Manager, cfg *domain.ProjectConfig, entry *domain.EnvironmentEntry, envName string) (*infraTarget, error) {
	if cfg.Infrastructure == nil || cfg.Infrastructure.ComposeFile == "" {
		return nil, fmt.Errorf("no infrastructure compose file configured in previewctl.yaml")
	}

	t := &infraTarget{
		composeFile: cfg.Infrastructure.ComposeFile,
		projectName: domain.ComposeProjectName(cfg.Name, envName),
		services:    cfg.InfraServices,
		envName:     envName,
	}

	var ports domain.PortMap
	if entry != nil {
		ports = entry.Ports
	}
	t.composeEnv = domain.BuildComposeEnv(cfg.Name, envName, ports)

	if entry != nil && entry.Compute != nil && entry.Compute.Type == "ssh" {
		t.ca = mgr.BuildSSHComputeAccess(entry)
		t.remote = true
		t.host = entry.Compute.Host
	} else {
		_, projectRoot, err := loadConfigWithMode(resolvedMode())
		if err != nil {
			return nil, err
		}
		absCompose := filepath.Join(projectRoot, cfg.Infrastructure.ComposeFile)
		if _, err := os.Stat(absCompose); err != nil {
			return nil, fmt.Errorf("infrastructure compose file not found: %s", absCompose)
		}
		t.ca = domain.NewDomainLocalComputeAccess(projectRoot)
	}

	return t, nil
}

// fetchComposePs runs `docker compose ... ps --all --format json` against the
// target's ComputeAccess and returns containers keyed by compose service name.
// This includes BOTH infra services and runner services, since the generated
// runner compose file and the user-provided infra compose file share the same
// docker compose project name — compose ps filters by project label.
func fetchComposePs(ctx context.Context, t *infraTarget) (map[string]composePsEntry, error) {
	psCmd := t.composeCmd("ps", "--all", "--format", "json")
	out, err := t.ca.Exec(ctx, psCmd, nil)
	if err != nil {
		return nil, err
	}
	entries, err := parseComposePs([]byte(out))
	if err != nil {
		return nil, err
	}
	byService := make(map[string]composePsEntry, len(entries))
	for _, e := range entries {
		byService[e.Service] = e
	}
	return byService, nil
}

// composeCmd builds a `docker compose -f <file> -p <project> <sub> <args...>`
// command string, with shell quoting applied so it is safe to pass through
// `sh -c` locally or via SSH remote exec.
func (t *infraTarget) composeCmd(sub string, args ...string) string {
	parts := []string{
		"docker", "compose",
		"-f", shellQuote(t.composeFile),
		"-p", shellQuote(t.projectName),
		sub,
	}
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

// targetLabel returns a short human description of where commands will run.
func (t *infraTarget) targetLabel() string {
	if t.remote {
		return fmt.Sprintf("remote (%s)", t.host)
	}
	return "local"
}

// shellQuote wraps a string in single quotes if it contains shell-sensitive characters.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9':
		case r == '/', r == '-', r == '_', r == '.', r == ':', r == '@', r == ',', r == '=':
		default:
			safe = false
		}
		if !safe {
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func newInfraStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start [service...]",
		Short: "Start infrastructure containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := resolveInfraTarget(cmd.Context())
			if err != nil {
				return err
			}
			composeCmd := t.composeCmd("up", append([]string{"-d"}, args...)...)
			if _, err := t.ca.VerboseExec(cmd.Context(), composeCmd, t.composeEnv); err != nil {
				return fmt.Errorf("starting infrastructure on %s: %w", t.targetLabel(), err)
			}
			Success(fmt.Sprintf("Infrastructure started on %s", t.targetLabel()))
			return nil
		},
	}
}

func newInfraStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop [service...]",
		Short: "Stop infrastructure containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := resolveInfraTarget(cmd.Context())
			if err != nil {
				return err
			}
			composeCmd := t.composeCmd("stop", args...)
			if _, err := t.ca.VerboseExec(cmd.Context(), composeCmd, t.composeEnv); err != nil {
				return fmt.Errorf("stopping infrastructure on %s: %w", t.targetLabel(), err)
			}
			Success(fmt.Sprintf("Infrastructure stopped on %s", t.targetLabel()))
			return nil
		},
	}
}

func newInfraRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart [service...]",
		Short: "Restart infrastructure containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := resolveInfraTarget(cmd.Context())
			if err != nil {
				return err
			}
			composeCmd := t.composeCmd("restart", args...)
			if _, err := t.ca.VerboseExec(cmd.Context(), composeCmd, t.composeEnv); err != nil {
				return fmt.Errorf("restarting infrastructure on %s: %w", t.targetLabel(), err)
			}
			Success(fmt.Sprintf("Infrastructure restarted on %s", t.targetLabel()))
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
			t, err := resolveInfraTarget(cmd.Context())
			if err != nil {
				return err
			}

			logsArgs := []string{}
			if follow {
				logsArgs = append(logsArgs, "-f")
			}
			if tail != "" {
				logsArgs = append(logsArgs, "--tail", tail)
			}
			if since != "" {
				logsArgs = append(logsArgs, "--since", since)
			}
			if until != "" {
				logsArgs = append(logsArgs, "--until", until)
			}
			if timestamps {
				logsArgs = append(logsArgs, "-t")
			}
			if noColor {
				logsArgs = append(logsArgs, "--no-color")
			}
			logsArgs = append(logsArgs, args...)

			composeCmd := t.composeCmd("logs", logsArgs...)
			if _, err := t.ca.VerboseExec(cmd.Context(), composeCmd, t.composeEnv); err != nil {
				return fmt.Errorf("fetching logs from %s: %w", t.targetLabel(), err)
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

// composePsEntry mirrors the fields we care about from `docker compose ps --format json`.
type composePsEntry struct {
	Name    string `json:"Name"`
	Service string `json:"Service"`
	Image   string `json:"Image"`
	State   string `json:"State"`
	Status  string `json:"Status"`
	Health  string `json:"Health"`
}

func newInfraStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status of infrastructure containers",
		Long: `List infrastructure services declared in the compose file and show
whether each container currently exists and is running. For remote environments
the check runs over SSH against the environment's compute host.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := resolveInfraTarget(cmd.Context())
			if err != nil {
				return err
			}

			psCmd := t.composeCmd("ps", "--all", "--format", "json")
			psOut, err := t.ca.Exec(cmd.Context(), psCmd, nil)
			if err != nil {
				return fmt.Errorf("listing infrastructure containers on %s: %w", t.targetLabel(), err)
			}

			entries, err := parseComposePs([]byte(psOut))
			if err != nil {
				return fmt.Errorf("parsing docker compose ps output: %w", err)
			}

			byService := make(map[string]composePsEntry, len(entries))
			for _, e := range entries {
				byService[e.Service] = e
			}

			// Only display services declared in the infrastructure compose file.
			// `docker compose ps -p <project>` also returns runner/service containers
			// that share the compose project name, which are not infra.
			names := make([]string, 0, len(t.services))
			for name := range t.services {
				names = append(names, name)
			}
			sort.Strings(names)

			if jsonOutput {
				type outRow struct {
					Service string `json:"service"`
					Status  string `json:"status"`
					Name    string `json:"name,omitempty"`
					State   string `json:"state,omitempty"`
					Image   string `json:"image,omitempty"`
					Detail  string `json:"detail,omitempty"`
					Health  string `json:"health,omitempty"`
				}
				rows := make([]outRow, 0, len(names))
				for _, name := range names {
					e, found := byService[name]
					row := outRow{Service: name, Status: "missing"}
					if found {
						row.Name = e.Name
						row.State = e.State
						row.Image = e.Image
						row.Detail = e.Status
						row.Health = e.Health
						switch strings.ToLower(e.State) {
						case "running":
							row.Status = "running"
						case "exited", "dead", "removing":
							row.Status = "stopped"
						case "":
							row.Status = "missing"
						default:
							row.Status = e.State
						}
					}
					rows = append(rows, row)
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}

			Header(fmt.Sprintf("Infrastructure %s", styleDetail.Render(t.projectName)))
			KeyValue("Target", t.targetLabel())

			if len(names) == 0 {
				fmt.Fprintf(os.Stderr, "\n  %s\n\n", styleDim.Render("No infrastructure services declared."))
				return nil
			}

			fmt.Fprintln(os.Stderr)
			for _, name := range names {
				e, found := byService[name]
				var badge, detail string
				switch {
				case !found:
					badge = StatusBadge("missing")
					detail = "not created"
				case strings.EqualFold(e.State, "running"):
					badge = StatusBadge("running")
					detail = e.Status
				default:
					badge = StatusBadge("stopped")
					if e.Status != "" {
						detail = e.Status
					} else {
						detail = e.State
					}
				}
				fmt.Fprintf(os.Stderr, "  %s  %s  %s  %s\n",
					styleMessage.Render(padRight(name, 14)),
					badge,
					styleDim.Render("·"),
					styleDim.Render(detail),
				)
			}
			fmt.Fprintln(os.Stderr)

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}

// parseComposePs parses the output of `docker compose ps --format json`.
// Compose v2.21+ emits newline-delimited JSON; older versions emit a JSON array.
func parseComposePs(out []byte) ([]composePsEntry, error) {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var entries []composePsEntry
		if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
			return nil, err
		}
		return entries, nil
	}
	var entries []composePsEntry
	for line := range strings.SplitSeq(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e composePsEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
