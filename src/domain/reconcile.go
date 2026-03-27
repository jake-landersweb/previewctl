package domain

import (
	"context"
	"fmt"
	"io"
	"time"
)

// ReconcileResult describes the outcome of reconciling a single step.
type ReconcileResult struct {
	Step       string
	Action     string // "ok", "healed", "failed", "skipped", "not_run"
	Message    string
	DurationMs int64
}

// ReconcileReport is the full outcome of a reconcile run.
type ReconcileReport struct {
	Results []ReconcileResult
	Healed  int
	Failed  int
	OK      int
	Skipped int
	NotRun  int
}

// Reconcile verifies all runner steps and re-executes any whose
// side effects are missing. Hook-owned steps are skipped since
// previewctl can't verify user-defined hooks.
//
// Progress is reported via the ProgressReporter for each step:
//   - StepStarted: "Verifying <step>..."
//   - StepCompleted: verification passed or heal succeeded
//   - StepFailed: heal failed
//   - StepSkipped: hook-owned or never completed
func (m *Manager) Reconcile(ctx context.Context, envName string) (*ReconcileReport, error) {
	entry, err := m.state.GetEnvironment(ctx, envName)
	if err != nil {
		return nil, fmt.Errorf("loading environment: %w", err)
	}
	if entry == nil {
		return nil, fmt.Errorf("environment '%s' not found", envName)
	}

	ca, manifest, err := m.loadManifestFromEntry(ctx, entry)
	if err != nil {
		return nil, err
	}

	// Wire stderr
	if setter, ok := ca.(interface{ SetStderr(io.Writer) }); ok {
		setter.SetStderr(m.progress.StderrWriter())
	}

	reg := newStepRegistry(m, entry, ca, manifest, envName, entry.Branch)

	// Hook-owned steps that previewctl can't verify
	hookOwned := map[string]bool{
		"runner_before":  true,
		"runner_deploy":  true,
		"runner_after":   true,
		"sync_code":      true,
		"build_services": true, // no verify — depends on turbo cache state
	}

	steps := m.BuildRunnerStepOrder()
	report := &ReconcileReport{}

	for _, stepName := range steps {
		// Steps that were never completed
		if !entry.StepCompleted(stepName) {
			m.progress.OnStep(StepEvent{
				Step:    stepName,
				Status:  StepSkipped,
				Message: stepName + " (never ran)",
			})
			report.Results = append(report.Results, ReconcileResult{
				Step:    stepName,
				Action:  "not_run",
				Message: "never completed",
			})
			report.NotRun++
			continue
		}

		// Hook-owned steps
		if hookOwned[stepName] {
			m.progress.OnStep(StepEvent{
				Step:    stepName,
				Status:  StepSkipped,
				Message: stepName + " (hook-owned)",
			})
			report.Results = append(report.Results, ReconcileResult{
				Step:    stepName,
				Action:  "skipped",
				Message: "hook-owned",
			})
			report.Skipped++
			continue
		}

		opts, err := reg.get(ctx, stepName)
		if err != nil {
			m.progress.OnStep(StepEvent{
				Step:    stepName,
				Status:  StepSkipped,
				Message: stepName + " (unknown)",
			})
			report.Results = append(report.Results, ReconcileResult{
				Step:    stepName,
				Action:  "skipped",
				Message: fmt.Sprintf("unknown: %v", err),
			})
			report.Skipped++
			continue
		}

		if opts.Verify == nil {
			m.progress.OnStep(StepEvent{
				Step:    stepName,
				Status:  StepSkipped,
				Message: stepName + " (no verify)",
			})
			report.Results = append(report.Results, ReconcileResult{
				Step:    stepName,
				Action:  "skipped",
				Message: "no verify function",
			})
			report.Skipped++
			continue
		}

		// Verify the step
		m.progress.OnStep(StepEvent{
			Step:    stepName,
			Status:  StepStarted,
			Message: fmt.Sprintf("Verifying %s...", stepName),
		})
		verifyStart := time.Now()

		if verifyErr := opts.Verify(ctx); verifyErr == nil {
			elapsed := time.Since(verifyStart)
			m.progress.OnStep(StepEvent{
				Step:    stepName,
				Status:  StepCompleted,
				Message: stepName + " healthy",
			})
			report.Results = append(report.Results, ReconcileResult{
				Step:       stepName,
				Action:     "ok",
				DurationMs: elapsed.Milliseconds(),
			})
			report.OK++
			continue
		}

		// Verification failed — heal
		m.progress.OnStep(StepEvent{
			Step:    stepName,
			Status:  StepStarted,
			Message: fmt.Sprintf("Healing %s...", stepName),
		})
		healStart := time.Now()

		if healErr := opts.Fn(); healErr != nil {
			elapsed := time.Since(healStart)
			m.progress.OnStep(StepEvent{
				Step:    stepName,
				Status:  StepFailed,
				Error:   healErr,
				Message: fmt.Sprintf("Failed to heal %s", stepName),
			})
			entry.AppendAudit(AuditEntry{
				Timestamp:  time.Now(),
				Step:       stepName,
				Action:     "reconcile_failed",
				Machine:    Hostname(),
				DurationMs: elapsed.Milliseconds(),
				Error:      healErr.Error(),
			})
			report.Results = append(report.Results, ReconcileResult{
				Step:       stepName,
				Action:     "failed",
				Message:    healErr.Error(),
				DurationMs: elapsed.Milliseconds(),
			})
			report.Failed++
			continue
		}

		elapsed := time.Since(healStart)
		m.progress.OnStep(StepEvent{
			Step:    stepName,
			Status:  StepCompleted,
			Message: fmt.Sprintf("Healed %s", stepName),
		})

		entry.SetStepRecord(&StepRecord{
			Name:       stepName,
			Status:     StepRecordCompleted,
			StartedAt:  healStart,
			FinishedAt: time.Now(),
			DurationMs: elapsed.Milliseconds(),
			Machine:    Hostname(),
		})
		entry.AppendAudit(AuditEntry{
			Timestamp:  time.Now(),
			Step:       stepName,
			Action:     "reconciled",
			Machine:    Hostname(),
			DurationMs: elapsed.Milliseconds(),
		})
		report.Results = append(report.Results, ReconcileResult{
			Step:       stepName,
			Action:     "healed",
			DurationMs: elapsed.Milliseconds(),
		})
		report.Healed++
	}

	// Persist updated state
	if err := m.state.SetEnvironment(ctx, envName, entry); err != nil {
		return report, fmt.Errorf("saving reconciled state: %w", err)
	}

	return report, nil
}
