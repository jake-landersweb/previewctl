package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jake-landersweb/previewctl/src/domain"
	"github.com/spf13/cobra"
)

func newStepsCmd() *cobra.Command {
	var audit bool

	cmd := &cobra.Command{
		Use:   "steps [name]",
		Short: "Show step-by-step status and audit log for an environment",
		Long: `Shows which provisioner and runner steps have completed, failed, or are
pending for an environment. Use --audit to see the full chronological
audit log of all actions taken.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, cfg, err := buildManager(nil)
			if err != nil {
				return err
			}

			home, _ := os.UserHomeDir()
			statePath := filepath.Join(home, ".cache", "previewctl", cfg.Name, "state.json")

			envName, err := resolveEnvName(args, statePath)
			if err != nil {
				return fmt.Errorf("could not determine environment: %w", err)
			}

			detail, err := mgr.Status(cmd.Context(), envName)
			if err != nil {
				return err
			}
			entry := detail.Entry

			if audit {
				return printAuditLog(entry)
			}
			return printSteps(mgr, entry)
		},
	}

	cmd.Flags().BoolVar(&audit, "audit", false, "Show full chronological audit log")

	return cmd
}

func printSteps(mgr *domain.Manager, entry *domain.EnvironmentEntry) error {
	fmt.Fprintf(os.Stderr, "\n")
	Header(fmt.Sprintf("Environment %s (%s)",
		styleDetail.Render(entry.Name),
		StatusBadge(string(entry.Status))))

	fmt.Fprintf(os.Stderr, "\n")
	SectionHeader("Provisioner Steps")
	provSteps := mgr.BuildProvisionerStepOrder()
	printStepList(entry, provSteps)

	fmt.Fprintf(os.Stderr, "\n")
	SectionHeader("Runner Steps")
	runSteps := mgr.BuildRunnerStepOrder()
	printStepList(entry, runSteps)

	fmt.Fprintln(os.Stderr)
	return nil
}

func printStepList(entry *domain.EnvironmentEntry, steps []string) {
	for _, stepName := range steps {
		if entry.Steps == nil {
			printPendingStep(stepName)
			continue
		}
		rec, ok := entry.Steps[stepName]
		if !ok {
			printPendingStep(stepName)
			continue
		}
		switch rec.Status {
		case domain.StepRecordCompleted:
			dur := formatMs(rec.DurationMs)
			ts := rec.FinishedAt.Format("2006-01-02 15:04:05")
			fmt.Fprintf(os.Stderr, "  %s %-24s %6s   %-16s %s\n",
				styleSuccess.Render("✓"),
				styleMessage.Render(stepName),
				styleDim.Render(dur),
				styleDim.Render(rec.Machine),
				styleDim.Render(ts))
		case domain.StepRecordFailed:
			dur := formatMs(rec.DurationMs)
			ts := rec.FinishedAt.Format("2006-01-02 15:04:05")
			fmt.Fprintf(os.Stderr, "  %s %-24s %6s   %-16s %s\n",
				styleFail.Render("✗"),
				styleFail.Render(stepName),
				styleDim.Render(dur),
				styleDim.Render(rec.Machine),
				styleDim.Render(ts))
			if rec.Error != "" {
				fmt.Fprintf(os.Stderr, "    %s %s\n",
					styleFail.Render("Error:"),
					styleDim.Render(rec.Error))
			}
		}
	}
}

func printPendingStep(name string) {
	fmt.Fprintf(os.Stderr, "  %s %-24s %6s   %-16s %s\n",
		styleDim.Render("·"),
		styleDim.Render(name),
		styleDim.Render("—"),
		styleDim.Render("—"),
		styleDim.Render("—"))
}

func printAuditLog(entry *domain.EnvironmentEntry) error {
	fmt.Fprintf(os.Stderr, "\n")
	Header(fmt.Sprintf("Audit log for %s", styleDetail.Render(entry.Name)))
	fmt.Fprintln(os.Stderr)

	if len(entry.AuditLog) == 0 {
		fmt.Fprintf(os.Stderr, "  %s\n\n", styleDim.Render("No audit entries."))
		return nil
	}

	for _, a := range entry.AuditLog {
		ts := a.Timestamp.Format("2006-01-02 15:04:05")
		dur := ""
		if a.DurationMs > 0 {
			dur = formatMs(a.DurationMs)
		}

		actionStyle := styleDim
		switch a.Action {
		case "executed":
			actionStyle = styleSuccess
		case "failed":
			actionStyle = styleFail
		case "skipped", "verified":
			actionStyle = styleSkipped
		case "verify_failed", "invalidated":
			actionStyle = styleSpinner
		}

		line := fmt.Sprintf("  %s  %-24s %-16s %-16s %6s",
			styleDim.Render(ts),
			styleMessage.Render(a.Step),
			actionStyle.Render(a.Action),
			styleDim.Render(a.Machine),
			styleDim.Render(dur))

		if a.Error != "" {
			line += "  " + styleFail.Render(a.Error)
		} else if a.Message != "" {
			line += "  " + styleDim.Render(a.Message)
		}

		fmt.Fprintln(os.Stderr, line)
	}
	fmt.Fprintln(os.Stderr)
	return nil
}

func formatMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	secs := float64(ms) / 1000.0
	if secs < 60 {
		return fmt.Sprintf("%.1fs", secs)
	}
	mins := int(secs / 60)
	remainSecs := secs - float64(mins*60)
	return fmt.Sprintf("%dm%.0fs", mins, remainSecs)
}
