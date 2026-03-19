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
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Print stderr on failure for debugging
		if stderr.Len() > 0 {
			fmt.Fprintf(os.Stderr, "\n%s", stderr.String())
		}
		return nil, fmt.Errorf("hook failed: %w", err)
	}

	// Print stderr after completion so it doesn't interfere with spinners
	if stderr.Len() > 0 {
		fmt.Fprintf(os.Stderr, "\n%s", stderr.String())
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
