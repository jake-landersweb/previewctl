package domain

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ExecuteCoreHook runs a core service hook script and captures KEY=VALUE outputs.
// stderr streams to the terminal in real-time for visibility.
// stdout is captured and parsed for KEY=VALUE pairs.
// Returns the captured outputs, validated against declaredOutputs.
func ExecuteCoreHook(ctx context.Context, hookScript string, declaredOutputs []string, env []string, workdir string) (map[string]string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", hookScript)
	cmd.Dir = workdir
	cmd.Env = env

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	// Stream stderr in real-time for visibility.
	// Caller should emit StepStreaming to stop the spinner before this runs.
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("hook failed: %w", err)
	}

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
