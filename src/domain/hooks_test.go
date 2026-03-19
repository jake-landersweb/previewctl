package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookRunner_BeforeAndAfter(t *testing.T) {
	tmpDir := t.TempDir()
	beforeFile := filepath.Join(tmpDir, "before.txt")
	afterFile := filepath.Join(tmpDir, "after.txt")

	hooks := HooksConfig{
		"test_step": {
			Before: []HookDef{{Run: "echo before > " + beforeFile}},
			After:  []HookDef{{Run: "echo after > " + afterFile}},
		},
	}

	runner := NewHookRunner(hooks, NoopReporter{})
	ctx := context.Background()
	hctx := &HookContext{EnvName: "test"}

	if err := runner.RunBefore(ctx, "test_step", hctx); err != nil {
		t.Fatalf("RunBefore failed: %v", err)
	}
	if _, err := os.Stat(beforeFile); err != nil {
		t.Error("expected before hook to create file")
	}

	if err := runner.RunAfter(ctx, "test_step", hctx); err != nil {
		t.Fatalf("RunAfter failed: %v", err)
	}
	if _, err := os.Stat(afterFile); err != nil {
		t.Error("expected after hook to create file")
	}
}

func TestHookRunner_NoHooks(t *testing.T) {
	runner := NewHookRunner(nil, NoopReporter{})
	ctx := context.Background()
	hctx := &HookContext{}

	// Should be no-ops
	if err := runner.RunBefore(ctx, "anything", hctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := runner.RunAfter(ctx, "anything", hctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHookRunner_FailureAborts(t *testing.T) {
	hooks := HooksConfig{
		"test_step": {
			Before: []HookDef{{Run: "exit 1"}},
		},
	}

	runner := NewHookRunner(hooks, NoopReporter{})
	ctx := context.Background()

	err := runner.RunBefore(ctx, "test_step", &HookContext{})
	if err == nil {
		t.Fatal("expected hook failure to return error")
	}
}

func TestHookRunner_ContinueOnError(t *testing.T) {
	tmpDir := t.TempDir()
	markerFile := filepath.Join(tmpDir, "ran.txt")

	hooks := HooksConfig{
		"test_step": {
			Before: []HookDef{
				{Run: "exit 1", ContinueOnError: true},
				{Run: "echo ok > " + markerFile},
			},
		},
	}

	runner := NewHookRunner(hooks, NoopReporter{})
	ctx := context.Background()

	err := runner.RunBefore(ctx, "test_step", &HookContext{})
	if err != nil {
		t.Fatalf("expected continueOnError to suppress failure, got: %v", err)
	}

	// Second hook should still have run
	if _, err := os.Stat(markerFile); err != nil {
		t.Error("expected second hook to run after first failed with continueOnError")
	}
}

func TestHookRunner_EnvironmentVariables(t *testing.T) {
	tmpDir := t.TempDir()
	envDump := filepath.Join(tmpDir, "env.txt")

	hooks := HooksConfig{
		"test_step": {
			Before: []HookDef{{Run: "env > " + envDump}},
		},
	}

	runner := NewHookRunner(hooks, NoopReporter{})
	ctx := context.Background()

	hctx := &HookContext{
		EnvName:      "feat-auth",
		Branch:       "feat/auth",
		ProjectName:  "myproject",
		ProjectRoot:  "/project",
		WorktreePath: tmpDir, // use real dir so hook can chdir
		Ports:        PortMap{"backend": 8042, "redis": 6421},
		CoreOutputs: map[string]map[string]string{
			"main": {
				"host":     "localhost",
				"port":     "5500",
				"user":     "postgres",
				"password": "secret",
				"database": "wt_feat_auth",
				"url":      "postgresql://postgres:secret@localhost:5500/wt_feat_auth",
			},
		},
	}

	if err := runner.RunBefore(ctx, "test_step", hctx); err != nil {
		t.Fatalf("RunBefore failed: %v", err)
	}

	data, err := os.ReadFile(envDump)
	if err != nil {
		t.Fatalf("reading env dump: %v", err)
	}
	env := string(data)

	expected := []string{
		"PREVIEWCTL_ENV_NAME=feat-auth",
		"PREVIEWCTL_BRANCH=feat/auth",
		"PREVIEWCTL_PROJECT_NAME=myproject",
		"PREVIEWCTL_PROJECT_ROOT=/project",
		"PREVIEWCTL_WORKTREE_PATH=" + tmpDir,
		"PREVIEWCTL_PORT_BACKEND=8042",
		"PREVIEWCTL_PORT_REDIS=6421",
		"PREVIEWCTL_CORE_MAIN_HOST=localhost",
		"PREVIEWCTL_CORE_MAIN_PORT=5500",
		"PREVIEWCTL_CORE_MAIN_USER=postgres",
		"PREVIEWCTL_CORE_MAIN_DATABASE=wt_feat_auth",
		"PREVIEWCTL_CORE_MAIN_URL=postgresql://postgres:secret@localhost:5500/wt_feat_auth",
		"PREVIEWCTL_STEP=test_step",
		"PREVIEWCTL_PHASE=before",
	}

	for _, exp := range expected {
		if !strings.Contains(env, exp) {
			t.Errorf("expected env to contain %s", exp)
		}
	}
}

func TestHookRunner_MultipleHooksRunInOrder(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "order.txt")

	hooks := HooksConfig{
		"test_step": {
			Before: []HookDef{
				{Run: "echo first >> " + logFile},
				{Run: "echo second >> " + logFile},
				{Run: "echo third >> " + logFile},
			},
		},
	}

	runner := NewHookRunner(hooks, NoopReporter{})
	ctx := context.Background()

	if err := runner.RunBefore(ctx, "test_step", &HookContext{}); err != nil {
		t.Fatalf("RunBefore failed: %v", err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}

	lines := strings.TrimSpace(string(data))
	expected := "first\nsecond\nthird"
	if lines != expected {
		t.Errorf("expected order %q, got %q", expected, lines)
	}
}

func TestHookRunner_WorkingDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	pwdFile := filepath.Join(tmpDir, "pwd.txt")

	hooks := HooksConfig{
		"test_step": {
			Before: []HookDef{{Run: "pwd > " + pwdFile}},
		},
	}

	runner := NewHookRunner(hooks, NoopReporter{})
	ctx := context.Background()

	hctx := &HookContext{WorktreePath: tmpDir}

	if err := runner.RunBefore(ctx, "test_step", hctx); err != nil {
		t.Fatalf("RunBefore failed: %v", err)
	}

	data, err := os.ReadFile(pwdFile)
	if err != nil {
		t.Fatalf("reading pwd: %v", err)
	}

	// Resolve symlinks for comparison (macOS /var -> /private/var)
	actualDir := strings.TrimSpace(string(data))
	resolvedTmp, _ := filepath.EvalSymlinks(tmpDir)
	resolvedActual, _ := filepath.EvalSymlinks(actualDir)
	if resolvedActual != resolvedTmp {
		t.Errorf("expected working directory %s, got %s", resolvedTmp, resolvedActual)
	}
}

func TestHookRunner_ProgressEvents(t *testing.T) {
	hooks := HooksConfig{
		"test_step": {
			Before: []HookDef{{Run: "true"}},
		},
	}

	progress := &mockProgressReporter{}
	runner := NewHookRunner(hooks, progress)
	ctx := context.Background()

	_ = runner.RunBefore(ctx, "test_step", &HookContext{})

	if len(progress.events) != 2 {
		t.Fatalf("expected 2 progress events (started + completed), got %d", len(progress.events))
	}
	if progress.events[0].Status != StepStarted {
		t.Errorf("expected first event to be started, got %s", progress.events[0].Status)
	}
	if progress.events[1].Status != StepCompleted {
		t.Errorf("expected second event to be completed, got %s", progress.events[1].Status)
	}
}
