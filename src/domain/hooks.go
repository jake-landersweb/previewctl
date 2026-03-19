package domain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// HooksConfig maps step names to their before/after hooks.
// Step names match the lifecycle steps: allocate_ports, create_compute,
// symlink_env, generate_env, start_infra, save_state, load_state,
// destroy_compute, cleanup_env, remove_state.
//
// Additionally, lifecycle-level hooks can be defined:
// create, delete — these run before/after the entire operation.
type HooksConfig map[string]StepHooks

// StepHooks defines before and after hooks for a step.
type StepHooks struct {
	Before []HookDef `yaml:"before,omitempty"`
	After  []HookDef `yaml:"after,omitempty"`
}

// HookDef defines a single hook to execute.
type HookDef struct {
	Run             string `yaml:"run"`
	ContinueOnError bool   `yaml:"continue_on_error,omitempty"`
}

// HookContext provides environment variables and working directory for hook execution.
type HookContext struct {
	EnvName      string
	Branch       string
	ProjectName  string
	ProjectRoot  string
	WorktreePath string
	Ports        PortMap
	CoreOutputs  map[string]map[string]string
	Step         string
	Phase        string // "before" or "after"
}

// HookRunner executes hooks defined in the config.
type HookRunner struct {
	hooks    HooksConfig
	progress ProgressReporter
}

// NewHookRunner creates a new hook runner.
func NewHookRunner(hooks HooksConfig, progress ProgressReporter) *HookRunner {
	if hooks == nil {
		hooks = make(HooksConfig)
	}
	return &HookRunner{hooks: hooks, progress: progress}
}

// RunBefore executes all "before" hooks for the given step.
func (r *HookRunner) RunBefore(ctx context.Context, step string, hctx *HookContext) error {
	return r.run(ctx, step, "before", hctx)
}

// RunAfter executes all "after" hooks for the given step.
func (r *HookRunner) RunAfter(ctx context.Context, step string, hctx *HookContext) error {
	return r.run(ctx, step, "after", hctx)
}

func (r *HookRunner) run(ctx context.Context, step string, phase string, hctx *HookContext) error {
	stepHooks, ok := r.hooks[step]
	if !ok {
		return nil
	}

	var hooks []HookDef
	if phase == "before" {
		hooks = stepHooks.Before
	} else {
		hooks = stepHooks.After
	}

	if len(hooks) == 0 {
		return nil
	}

	hctx.Step = step
	hctx.Phase = phase
	env := r.buildEnv(hctx)

	for _, hook := range hooks {
		hookLabel := fmt.Sprintf("hook:%s:%s", phase, step)
		r.progress.OnStep(StepEvent{
			Step:    hookLabel,
			Status:  StepStarted,
			Message: fmt.Sprintf("Running %s hook: %s", phase, truncate(hook.Run, 60)),
		})

		err := executeHook(ctx, hook.Run, env, hctx.WorktreePath)
		if err != nil {
			if hook.ContinueOnError {
				r.progress.OnStep(StepEvent{
					Step:    hookLabel,
					Status:  StepSkipped,
					Message: fmt.Sprintf("Hook failed (continuing): %v", err),
				})
				continue
			}
			r.progress.OnStep(StepEvent{
				Step:   hookLabel,
				Status: StepFailed,
				Error:  fmt.Errorf("hook '%s' failed: %w", truncate(hook.Run, 40), err),
			})
			return fmt.Errorf("hook '%s' failed: %w", truncate(hook.Run, 40), err)
		}

		r.progress.OnStep(StepEvent{
			Step:    hookLabel,
			Status:  StepCompleted,
			Message: fmt.Sprintf("Hook completed: %s", truncate(hook.Run, 60)),
		})
	}

	return nil
}

func (r *HookRunner) buildEnv(hctx *HookContext) []string {
	env := os.Environ()

	set := func(key, value string) {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	set("PREVIEWCTL_STEP", hctx.Step)
	set("PREVIEWCTL_PHASE", hctx.Phase)

	if hctx.EnvName != "" {
		set("PREVIEWCTL_ENV_NAME", hctx.EnvName)
	}
	if hctx.Branch != "" {
		set("PREVIEWCTL_BRANCH", hctx.Branch)
	}
	if hctx.ProjectName != "" {
		set("PREVIEWCTL_PROJECT_NAME", hctx.ProjectName)
	}
	if hctx.ProjectRoot != "" {
		set("PREVIEWCTL_PROJECT_ROOT", hctx.ProjectRoot)
	}
	if hctx.WorktreePath != "" {
		set("PREVIEWCTL_WORKTREE_PATH", hctx.WorktreePath)
	}

	for name, port := range hctx.Ports {
		envKey := fmt.Sprintf("PREVIEWCTL_PORT_%s", strings.ToUpper(strings.ReplaceAll(name, "-", "_")))
		set(envKey, fmt.Sprintf("%d", port))
	}

	for svcName, outputs := range hctx.CoreOutputs {
		prefix := "PREVIEWCTL_CORE_" + strings.ToUpper(strings.ReplaceAll(svcName, "-", "_")) + "_"
		for key, val := range outputs {
			env = append(env, prefix+strings.ToUpper(key)+"="+val)
		}
	}

	return env
}

func executeHook(ctx context.Context, command string, env []string, workdir string) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if workdir != "" {
		cmd.Dir = workdir
	}

	return cmd.Run()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
