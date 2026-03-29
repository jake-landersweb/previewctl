package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/jake-landersweb/previewctl/src/domain"
)

var (
	// Colors
	colorGreen   = lipgloss.Color("#34D399")
	colorRed     = lipgloss.Color("#F87171")
	colorYellow  = lipgloss.Color("#FBBF24")
	colorBlue    = lipgloss.Color("#60A5FA")
	colorDim     = lipgloss.Color("#6B7280")
	colorCyan    = lipgloss.Color("#22D3EE")
	colorWhite   = lipgloss.Color("#F9FAFB")
	colorMagenta = lipgloss.Color("#C084FC")

	// Styles
	styleSuccess  = lipgloss.NewStyle().Foreground(colorGreen)
	styleFail     = lipgloss.NewStyle().Foreground(colorRed)
	styleSkipped  = lipgloss.NewStyle().Foreground(colorYellow)
	styleSpinner  = lipgloss.NewStyle().Foreground(colorCyan)
	styleDim      = lipgloss.NewStyle().Foreground(colorDim)
	styleMessage  = lipgloss.NewStyle().Foreground(colorWhite)
	styleDuration = lipgloss.NewStyle().Foreground(colorDim)
	styleDetail   = lipgloss.NewStyle().Foreground(colorMagenta)

	// Spinner frames
	spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
)

// CLIProgressReporter renders lifecycle events with colors, spinners, and timing.
type CLIProgressReporter struct {
	stepStart time.Time
	spinner   *liveSpinner
	streaming bool          // true after StepStreaming received
	stderr    *indentWriter // indents hook output
}

// NewCLIProgressReporter creates a new CLI progress reporter.
func NewCLIProgressReporter() *CLIProgressReporter {
	return &CLIProgressReporter{
		stderr: newIndentWriter(os.Stderr, "    "),
	}
}

// StderrWriter returns a writer that indents hook output for display below the step indicator.
func (r *CLIProgressReporter) StderrWriter() io.Writer {
	return r.stderr
}

func (r *CLIProgressReporter) OnStep(event domain.StepEvent) {
	switch event.Status {
	case domain.StepStarted:
		r.stepStart = time.Now()
		r.streaming = false
		// Stop any previous spinner
		if r.spinner != nil {
			r.spinner.Stop()
		}
		r.spinner = newLiveSpinner(event.Message)
		r.spinner.Start()

	case domain.StepStreaming:
		// Transition from animated spinner to static indicator
		if r.spinner != nil {
			r.spinner.Stop()
			r.spinner = nil
		}
		r.streaming = true
		// Print static indicator line for the step
		msg := event.Message
		if msg == "" {
			msg = event.Step
		}
		fmt.Fprintf(os.Stderr, "\r  %s %s\n",
			styleDim.Render("→"),
			styleMessage.Render(msg),
		)

	case domain.StepCompleted:
		if r.spinner != nil {
			r.spinner.Stop()
			r.spinner = nil
		}
		elapsed := time.Since(r.stepStart)
		msg := event.Message
		if msg == "" {
			msg = "Done"
		}
		if r.streaming {
			// Output was printed below the static indicator — print completion as new line
			fmt.Fprintf(os.Stderr, "  %s %s %s\n",
				styleSuccess.Render("✓"),
				styleMessage.Render(msg),
				styleDuration.Render(formatDuration(elapsed)),
			)
		} else {
			// Pure compute step — overwrite the spinner line in place
			fmt.Fprintf(os.Stderr, "\r  %s %s %s\n",
				styleSuccess.Render("✓"),
				styleMessage.Render(msg),
				styleDuration.Render(formatDuration(elapsed)),
			)
		}
		r.streaming = false

	case domain.StepFailed:
		if r.spinner != nil {
			r.spinner.Stop()
			r.spinner = nil
		}
		if r.streaming {
			fmt.Fprintf(os.Stderr, "  %s %s\n",
				styleFail.Render("✗"),
				styleFail.Render(fmt.Sprintf("Failed: %v", event.Error)),
			)
		} else {
			fmt.Fprintf(os.Stderr, "\r  %s %s\n",
				styleFail.Render("✗"),
				styleFail.Render(fmt.Sprintf("Failed: %v", event.Error)),
			)
		}
		r.streaming = false

	case domain.StepSkipped:
		if r.spinner != nil {
			r.spinner.Stop()
			r.spinner = nil
		}
		fmt.Fprintf(os.Stderr, "\r  %s %s\n",
			styleSkipped.Render("−"),
			styleDim.Render(event.Message),
		)
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("(%dms)", d.Milliseconds())
	}
	return fmt.Sprintf("(%.1fs)", d.Seconds())
}

// ---------- indentWriter ----------

// indentWriter wraps an io.Writer and prefixes each line with an indent string.
type indentWriter struct {
	w      io.Writer
	indent string
	atBOL  bool // true when at beginning of line
	mu     sync.Mutex
}

func newIndentWriter(w io.Writer, indent string) *indentWriter {
	return &indentWriter{w: w, indent: indent, atBOL: true}
}

func (iw *indentWriter) Write(p []byte) (n int, err error) {
	iw.mu.Lock()
	defer iw.mu.Unlock()

	written := 0
	for len(p) > 0 {
		if iw.atBOL {
			if _, err := iw.w.Write([]byte(iw.indent)); err != nil {
				return written, err
			}
			iw.atBOL = false
		}
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			n, err := iw.w.Write(p)
			return written + n, err
		}
		n, err := iw.w.Write(p[:idx+1])
		written += n
		if err != nil {
			return written, err
		}
		p = p[idx+1:]
		iw.atBOL = true
	}
	return written, nil
}

// ---------- liveSpinner ----------

// liveSpinner animates a spinner on the current line until stopped.
type liveSpinner struct {
	message string
	stop    chan struct{}
	done    chan struct{}
	mu      sync.Mutex
}

func newLiveSpinner(message string) *liveSpinner {
	return &liveSpinner{
		message: message,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (s *liveSpinner) Start() {
	go func() {
		defer close(s.done)
		frame := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		// Render first frame immediately
		s.render(frame)
		frame++

		for {
			select {
			case <-s.stop:
				s.clear()
				return
			case <-ticker.C:
				s.render(frame)
				frame++
			}
		}
	}()
}

func (s *liveSpinner) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	select {
	case <-s.stop:
		// already stopped
	default:
		close(s.stop)
	}
	<-s.done
}

func (s *liveSpinner) render(frame int) {
	f := spinnerFrames[frame%len(spinnerFrames)]
	line := fmt.Sprintf("  %s %s",
		styleSpinner.Render(f),
		styleMessage.Render(s.message),
	)
	fmt.Fprintf(os.Stderr, "\r%s", line)
}

func (s *liveSpinner) clear() {
	// Clear the line
	fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 80))
}

// --- Styled output helpers for commands ---

// Header prints a styled command header.
func Header(text string) {
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(colorWhite)
	fmt.Fprintf(os.Stderr, "\n%s\n\n", style.Render(text))
}

// Success prints a styled success message.
func Success(text string) {
	fmt.Fprintf(os.Stderr, "\n%s %s\n\n",
		styleSuccess.Render("✓"),
		lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render(text),
	)
}

// KeyValue prints a styled key-value pair.
func KeyValue(key string, value string) {
	fmt.Fprintf(os.Stderr, "  %s %s\n",
		styleDim.Render(key+":"),
		styleMessage.Render(value),
	)
}

// DetailKeyValue prints a styled detail key-value pair with indentation.
func DetailKeyValue(key string, value string) {
	fmt.Fprintf(os.Stderr, "    %s %s\n",
		styleDim.Render(key),
		styleDetail.Render(value),
	)
}

// SectionHeader prints a styled section header.
func SectionHeader(text string) {
	fmt.Fprintf(os.Stderr, "  %s\n",
		lipgloss.NewStyle().Bold(true).Foreground(colorBlue).Render(text),
	)
}

// PrintServiceURLs prints a "Services" section with compiled URLs or localhost fallbacks.
func PrintServiceURLs(envName string, ports domain.PortMap, proxyDomain string) {
	SectionHeader("Services")
	portNames := make([]string, 0, len(ports))
	for name := range ports {
		portNames = append(portNames, name)
	}
	sort.Strings(portNames)
	for _, name := range portNames {
		port := ports[name]
		var url string
		if proxyDomain != "" {
			url = fmt.Sprintf("https://%s--%s.%s", envName, name, proxyDomain)
		} else {
			url = fmt.Sprintf("http://localhost:%d", port)
		}
		DetailKeyValue(name, url)
	}
}

// StatusBadge returns a colored status string.
func StatusBadge(status string) string {
	switch status {
	case "running":
		return styleSuccess.Render("● running")
	case "stopped":
		return styleFail.Render("○ stopped")
	case "creating":
		return lipgloss.NewStyle().Foreground(colorYellow).Render("◌ creating")
	case "error":
		return styleFail.Render("✗ error")
	case "exists":
		return styleSuccess.Render("● exists")
	case "missing":
		return styleFail.Render("○ missing")
	default:
		return styleDim.Render(status)
	}
}
