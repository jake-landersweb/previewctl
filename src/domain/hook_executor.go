package domain

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// ExecuteCoreHook runs a core service hook script and captures KEY=VALUE outputs.
// Both stdout and stderr stream to the terminal in real-time for visibility.
// KEY=VALUE lines on stdout are redacted (value replaced with ********) before display.
// stdout is also captured and parsed for KEY=VALUE pairs.
// Returns the captured outputs, validated against declaredOutputs.
func ExecuteCoreHook(ctx context.Context, hookScript string, declaredOutputs []string, env []string, workdir string, stderr io.Writer) (map[string]string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", hookScript)
	cmd.Dir = workdir
	cmd.Env = env

	var stdout bytes.Buffer
	sw := &sanitizingWriter{dest: stderr}
	cmd.Stdout = io.MultiWriter(&stdout, sw)

	// Stream stderr through the provided writer for visibility.
	// Caller should emit StepStreaming to stop the spinner before this runs.
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		_ = sw.Flush()
		return nil, fmt.Errorf("hook failed: %w", err)
	}
	_ = sw.Flush()

	outputs := parseHookOutput(stdout.String())

	// Validate all declared outputs are present
	var missing []string
	for _, key := range declaredOutputs {
		if _, ok := outputs[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("hook did not produce required outputs: %s", strings.Join(missing, ", "))
	}

	return outputs, nil
}

// sanitizingWriter wraps a destination writer and redacts values from KEY=VALUE
// lines before forwarding. Non-KEY=VALUE lines pass through unchanged.
type sanitizingWriter struct {
	dest io.Writer
	buf  []byte
}

func (w *sanitizingWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(w.buf[:idx])
		w.buf = w.buf[idx+1:]

		sanitized := sanitizeOutputLine(line)
		if _, err := fmt.Fprintln(w.dest, sanitized); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

// Flush writes any remaining buffered content (final line without trailing newline).
func (w *sanitizingWriter) Flush() error {
	if len(w.buf) > 0 {
		line := string(w.buf)
		w.buf = nil
		_, err := fmt.Fprintln(w.dest, sanitizeOutputLine(line))
		return err
	}
	return nil
}

// sanitizeOutputLine redacts the value portion of KEY=VALUE lines.
func sanitizeOutputLine(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return line
	}
	key, _, ok := strings.Cut(trimmed, "=")
	if !ok {
		return line
	}
	return strings.TrimSpace(key) + "=********"
}

// extractGlobalOutputs returns outputs with the GLOBAL_ prefix, stripping the
// prefix from the key. These are auto-persisted to the environment store.
func extractGlobalOutputs(outputs map[string]string) map[string]string {
	globals := make(map[string]string)
	for key, value := range outputs {
		if strings.HasPrefix(key, "GLOBAL_") {
			globals[strings.TrimPrefix(key, "GLOBAL_")] = value
		}
	}
	return globals
}

// parseHookOutput parses KEY=VALUE lines from hook stdout.
// Blank lines and lines starting with # are skipped.
func parseHookOutput(output string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		result[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return result
}
