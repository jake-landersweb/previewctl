package cli

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/jake-landersweb/previewctl/src/domain"
)

// captureStderr redirects os.Stderr to a pipe, runs fn, and returns what was written.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}

	orig := os.Stderr
	os.Stderr = w

	fn()

	_ = w.Close()
	os.Stderr = orig

	buf := make([]byte, 64*1024)
	n, _ := r.Read(buf)
	_ = r.Close()
	return string(buf[:n])
}

// hasAnsi returns true if s contains ANSI escape sequences.
func hasAnsi(s string) bool {
	return strings.Contains(s, "\x1b[")
}

// setCIMode sets the global CI mode state for testing and returns a cleanup function.
func setCIMode(mode bool, provider ciProvider) func() {
	prevCI := ciMode
	prevProvider := detectedProvider
	ciMode = mode
	detectedProvider = provider
	return func() {
		ciMode = prevCI
		detectedProvider = prevProvider
	}
}

// saveStyles snapshots the current styles and returns a restore function.
func saveStyles() func() {
	saved := [8]lipgloss.Style{
		styleSuccess, styleFail, styleSkipped, styleSpinner,
		styleDim, styleMessage, styleDuration, styleDetail,
	}
	return func() {
		styleSuccess = saved[0]
		styleFail = saved[1]
		styleSkipped = saved[2]
		styleSpinner = saved[3]
		styleDim = saved[4]
		styleMessage = saved[5]
		styleDuration = saved[6]
		styleDetail = saved[7]
	}
}

// --- Output helper tests ---

func TestHeader_Interactive(t *testing.T) {
	cleanup := setCIMode(false, ciProviderNone)
	defer cleanup()

	out := captureStderr(t, func() { Header("Creating environment") })

	if !strings.Contains(out, "Creating environment") {
		t.Errorf("expected text in output, got: %q", out)
	}
	// Interactive mode should have decorative newlines
	if !strings.HasPrefix(out, "\n") {
		t.Errorf("expected leading newline in interactive mode, got: %q", out)
	}
}

func TestHeader_CI(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGeneric)
	defer cleanup()

	out := captureStderr(t, func() { Header("Creating environment") })

	if out != "== Creating environment\n" {
		t.Errorf("expected plain header, got: %q", out)
	}
	if hasAnsi(out) {
		t.Errorf("CI output should not contain ANSI codes, got: %q", out)
	}
}

func TestHeader_GitHubActions(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGitHub)
	defer cleanup()
	ciGroupOpen = false

	out := captureStderr(t, func() { Header("Creating environment") })

	if !strings.Contains(out, "::group::Creating environment") {
		t.Errorf("expected ::group:: command, got: %q", out)
	}
	if !ciGroupOpen {
		t.Error("expected ciGroupOpen to be true after Header")
	}
	// Clean up
	ciGroupOpen = false
}

func TestHeader_GitHubActions_ClosesOpenGroup(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGitHub)
	defer cleanup()
	ciGroupOpen = false

	out := captureStderr(t, func() {
		Header("First phase")
		Header("Second phase")
	})

	if !strings.Contains(out, "::endgroup::") {
		t.Errorf("expected ::endgroup:: when opening second group, got: %q", out)
	}
	// Should have both groups
	if strings.Count(out, "::group::") != 2 {
		t.Errorf("expected 2 ::group:: commands, got: %q", out)
	}
	ciGroupOpen = false
}

func TestSuccess_CI(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGeneric)
	defer cleanup()

	out := captureStderr(t, func() { Success("Environment ready") })

	if out != "[OK] Environment ready\n" {
		t.Errorf("expected plain success, got: %q", out)
	}
	if hasAnsi(out) {
		t.Errorf("CI output should not contain ANSI codes, got: %q", out)
	}
}

func TestSuccess_GitHubActions(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGitHub)
	defer cleanup()
	ciGroupOpen = true

	out := captureStderr(t, func() { Success("Environment ready") })

	if !strings.Contains(out, "::endgroup::") {
		t.Errorf("expected ::endgroup:: before notice, got: %q", out)
	}
	if !strings.Contains(out, "::notice::Environment ready") {
		t.Errorf("expected ::notice:: command, got: %q", out)
	}
	ciGroupOpen = false
}

func TestKeyValue_CI(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGeneric)
	defer cleanup()

	out := captureStderr(t, func() { KeyValue("Branch", "main") })

	if out != "  Branch: main\n" {
		t.Errorf("expected plain key-value, got: %q", out)
	}
}

func TestDetailKeyValue_CI(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGeneric)
	defer cleanup()

	out := captureStderr(t, func() { DetailKeyValue("api", "http://localhost:8080") })

	if out != "    api http://localhost:8080\n" {
		t.Errorf("expected plain detail, got: %q", out)
	}
}

func TestSectionHeader_CI(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGeneric)
	defer cleanup()

	out := captureStderr(t, func() { SectionHeader("Services") })

	if out != "-- Services\n" {
		t.Errorf("expected plain section header, got: %q", out)
	}
}

func TestStatusBadge_CI(t *testing.T) {
	cleanup := setCIMode(true, ciProviderGeneric)
	defer cleanup()

	tests := []struct {
		input    string
		expected string
	}{
		{"running", "running"},
		{"stopped", "stopped"},
		{"creating", "creating"},
		{"error", "error"},
		{"exists", "exists"},
		{"missing", "missing"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		got := StatusBadge(tt.input)
		if got != tt.expected {
			t.Errorf("StatusBadge(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestStatusBadge_Interactive(t *testing.T) {
	cleanup := setCIMode(false, ciProviderNone)
	defer cleanup()

	got := StatusBadge("running")
	if !strings.Contains(got, "running") {
		t.Errorf("expected 'running' in output, got: %q", got)
	}
	// Interactive should have the unicode bullet
	if !strings.Contains(got, "●") {
		t.Errorf("expected unicode bullet in interactive mode, got: %q", got)
	}
}

// --- CI output should never contain ANSI ---

func TestCIOutput_NoAnsiCodes(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGeneric)
	defer cleanup()

	out := captureStderr(t, func() {
		Header("Test header")
		Success("Test success")
		KeyValue("key", "value")
		DetailKeyValue("detail", "value")
		SectionHeader("Section")
	})

	if hasAnsi(out) {
		t.Errorf("CI output should not contain ANSI escape codes, got:\n%s", out)
	}
}

// --- ProgressReporter tests ---

func TestProgressReporter_CI_StepLifecycle(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGeneric)
	defer cleanup()

	reporter := NewCLIProgressReporter()

	out := captureStderr(t, func() {
		reporter.OnStep(domain.StepEvent{
			Step:    "create_worktree",
			Status:  domain.StepStarted,
			Message: "Creating worktree",
		})
		reporter.OnStep(domain.StepEvent{
			Step:    "create_worktree",
			Status:  domain.StepCompleted,
			Message: "Worktree created",
		})
	})

	if !strings.Contains(out, "... Creating worktree") {
		t.Errorf("expected start message, got: %q", out)
	}
	if !strings.Contains(out, "[OK] Worktree created") {
		t.Errorf("expected completion message, got: %q", out)
	}
	if hasAnsi(out) {
		t.Errorf("CI output should not contain ANSI codes, got: %q", out)
	}
}

func TestProgressReporter_CI_StepFailed(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGeneric)
	defer cleanup()

	reporter := NewCLIProgressReporter()

	out := captureStderr(t, func() {
		reporter.OnStep(domain.StepEvent{
			Step:    "seed_db",
			Status:  domain.StepStarted,
			Message: "Seeding database",
		})
		reporter.OnStep(domain.StepEvent{
			Step:   "seed_db",
			Status: domain.StepFailed,
			Error:  fmt.Errorf("connection refused"),
		})
	})

	if !strings.Contains(out, "[FAIL] Failed: connection refused") {
		t.Errorf("expected failure message, got: %q", out)
	}
}

func TestProgressReporter_CI_StepSkipped(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGeneric)
	defer cleanup()

	reporter := NewCLIProgressReporter()

	out := captureStderr(t, func() {
		reporter.OnStep(domain.StepEvent{
			Step:    "run_hook",
			Status:  domain.StepSkipped,
			Message: "No hook configured",
		})
	})

	if !strings.Contains(out, "[SKIP] No hook configured") {
		t.Errorf("expected skip message, got: %q", out)
	}
}

func TestProgressReporter_GitHubActions_StepFailed(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGitHub)
	defer cleanup()

	reporter := NewCLIProgressReporter()

	out := captureStderr(t, func() {
		reporter.OnStep(domain.StepEvent{
			Step:    "seed_db",
			Status:  domain.StepStarted,
			Message: "Seeding database",
		})
		reporter.OnStep(domain.StepEvent{
			Step:   "seed_db",
			Status: domain.StepFailed,
			Error:  fmt.Errorf("connection refused"),
		})
	})

	if !strings.Contains(out, "::error title=seed_db::Failed: connection refused") {
		t.Errorf("expected GitHub Actions error annotation, got: %q", out)
	}
}

func TestProgressReporter_GitHubActions_StepSkipped(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGitHub)
	defer cleanup()

	reporter := NewCLIProgressReporter()

	out := captureStderr(t, func() {
		reporter.OnStep(domain.StepEvent{
			Step:    "run_hook",
			Status:  domain.StepSkipped,
			Message: "No hook configured",
		})
	})

	if !strings.Contains(out, "::warning title=run_hook::No hook configured") {
		t.Errorf("expected GitHub Actions warning annotation, got: %q", out)
	}
}

func TestProgressReporter_CI_Streaming(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGeneric)
	defer cleanup()

	reporter := NewCLIProgressReporter()

	out := captureStderr(t, func() {
		reporter.OnStep(domain.StepEvent{
			Step:    "run_hook",
			Status:  domain.StepStarted,
			Message: "Running hook",
		})
		reporter.OnStep(domain.StepEvent{
			Step:    "run_hook",
			Status:  domain.StepStreaming,
			Message: "Streaming output",
		})
		reporter.OnStep(domain.StepEvent{
			Step:    "run_hook",
			Status:  domain.StepCompleted,
			Message: "Hook completed",
		})
	})

	if !strings.Contains(out, "-- Streaming output") {
		t.Errorf("expected streaming indicator, got: %q", out)
	}
	if !strings.Contains(out, "[OK] Hook completed") {
		t.Errorf("expected completion after streaming, got: %q", out)
	}
}

// --- Spinner tests ---

func TestSpinner_CI_NoAnimation(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()
	disableStyles()
	cleanup := setCIMode(true, ciProviderGeneric)
	defer cleanup()

	out := captureStderr(t, func() {
		s := newLiveSpinner("Loading...")
		s.Start()
		s.Stop()
	})

	if out != "  ... Loading...\n" {
		t.Errorf("expected single line output, got: %q", out)
	}
	// Should not contain carriage return (spinner animation artifact)
	if strings.Contains(out, "\r") {
		t.Errorf("CI spinner should not contain carriage returns, got: %q", out)
	}
}

// --- endCIGroup tests ---

func TestEndCIGroup_ClosesOpenGroup(t *testing.T) {
	cleanup := setCIMode(true, ciProviderGitHub)
	defer cleanup()
	ciGroupOpen = true

	out := captureStderr(t, func() { endCIGroup() })

	if out != "::endgroup::\n" {
		t.Errorf("expected ::endgroup::, got: %q", out)
	}
	if ciGroupOpen {
		t.Error("expected ciGroupOpen to be false after endCIGroup")
	}
}

func TestEndCIGroup_NoopWhenClosed(t *testing.T) {
	cleanup := setCIMode(true, ciProviderGitHub)
	defer cleanup()
	ciGroupOpen = false

	out := captureStderr(t, func() { endCIGroup() })

	if out != "" {
		t.Errorf("expected no output when group already closed, got: %q", out)
	}
}

func TestEndCIGroup_NoopGenericProvider(t *testing.T) {
	cleanup := setCIMode(true, ciProviderGeneric)
	defer cleanup()
	ciGroupOpen = true // shouldn't matter for generic

	out := captureStderr(t, func() { endCIGroup() })

	if out != "" {
		t.Errorf("expected no output for generic provider, got: %q", out)
	}
	// Reset
	ciGroupOpen = false
}

// --- disableStyles test ---

func TestDisableStyles_RendersPassthrough(t *testing.T) {
	restoreStyles := saveStyles()
	defer restoreStyles()

	disableStyles()

	// All styles should pass through text unchanged (no ANSI)
	styles := []struct {
		name  string
		style lipgloss.Style
	}{
		{"success", styleSuccess},
		{"fail", styleFail},
		{"skipped", styleSkipped},
		{"spinner", styleSpinner},
		{"dim", styleDim},
		{"message", styleMessage},
		{"duration", styleDuration},
		{"detail", styleDetail},
	}
	for _, s := range styles {
		rendered := s.style.Render("test")
		if rendered != "test" {
			t.Errorf("style %s should be passthrough after disableStyles, got: %q", s.name, rendered)
		}
	}
}
