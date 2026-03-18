package cli

import (
	"fmt"
	"time"

	"github.com/jake/previewctl/src/domain"
)

// CLIProgressReporter renders lifecycle events as formatted terminal output.
type CLIProgressReporter struct {
	stepStart time.Time
}

// NewCLIProgressReporter creates a new CLI progress reporter.
func NewCLIProgressReporter() *CLIProgressReporter {
	return &CLIProgressReporter{}
}

func (r *CLIProgressReporter) OnStep(event domain.StepEvent) {
	switch event.Status {
	case domain.StepStarted:
		r.stepStart = time.Now()
		fmt.Printf("  ▸ %s\n", event.Message)
	case domain.StepCompleted:
		elapsed := time.Since(r.stepStart)
		if event.Message != "" {
			fmt.Printf("  ✓ %s (%s)\n", event.Message, formatDuration(elapsed))
		} else {
			fmt.Printf("  ✓ Done (%s)\n", formatDuration(elapsed))
		}
	case domain.StepFailed:
		fmt.Printf("  ✗ Failed: %v\n", event.Error)
	case domain.StepSkipped:
		fmt.Printf("  - Skipped: %s\n", event.Message)
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
