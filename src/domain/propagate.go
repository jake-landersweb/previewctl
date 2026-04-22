package domain

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// propagateProvisionerChange brings the remote VM in line with fresh values
// that were just written to entry.ProvisionerOutputs[provisionerName] and/or
// entry.Env (when the hook exported GLOBAL_* values that changed).
//
// Intended to run after a provisioner reset/init so dependent services pick up
// new credentials or store values without the user having to manually re-run
// generate_manifest + generate_env and restart containers.
//
// changedStoreKeys: the set of entry.Env keys whose value changed during the
// triggering hook run. Services whose env/Start template references
// "{{store.<key>}}" for any changed key are restarted too. Pass nil/empty when
// nothing in the store changed.
//
// Side effects on the VM:
//  1. generate_manifest  — rewrites .previewctl.json from current state
//  2. generate_env       — rewrites per-service .env / .env.local files
//  3. generate_compose   — only if any Start command references a changed value
//  4. docker compose restart/up -d for services affected by the change
//
// Returns nil for local environments (nothing to push), and for remote envs
// where no enabled service is affected (env files are still regenerated).
func (m *Manager) propagateProvisionerChange(ctx context.Context, envName, provisionerName string, changedStoreKeys []string) error {
	entry, err := m.state.GetEnvironment(ctx, envName)
	if err != nil {
		return fmt.Errorf("loading environment for propagation: %w", err)
	}
	if entry == nil {
		return fmt.Errorf("environment '%s' not found", envName)
	}
	if entry.Compute == nil || entry.Compute.Type != "ssh" {
		return nil
	}

	needsCompose, affected := servicesAffectedByChange(m.config, provisionerName, changedStoreKeys)

	steps := []string{"generate_manifest", "generate_env"}
	if needsCompose {
		steps = append(steps, "generate_compose")
	}
	if err := m.RunSteps(ctx, envName, steps); err != nil {
		return fmt.Errorf("regenerating remote config after %s change: %w", provisionerName, err)
	}

	// Intersect affected with currently-enabled services so we don't boot
	// anything the user deliberately stopped.
	var toRestart []string
	for _, name := range entry.EnabledServices {
		if affected[name] {
			toRestart = append(toRestart, name)
		}
	}
	if len(toRestart) == 0 {
		return nil
	}
	sort.Strings(toRestart)

	ca := m.BuildSSHComputeAccess(entry)
	// Use `up -d` when compose config may have changed — it reconciles
	// the service to the new config (no-ops if unchanged). For plain env
	// changes `restart` is sufficient and cheaper.
	verb := "restart"
	if needsCompose {
		verb = "up -d"
	}
	projectName := ComposeProjectName(m.config.Name, envName)
	cmd := fmt.Sprintf("docker compose -f .previewctl.compose.yaml -p %s %s %s",
		projectName, verb, strings.Join(toRestart, " "))
	if _, err := ca.VerboseExec(ctx, cmd, nil); err != nil {
		return fmt.Errorf("restarting services after %s change: %w", provisionerName, err)
	}
	return nil
}

// SyncRemote regenerates every VM-side artifact we know how to regenerate
// (manifest, env files, compose, nginx) from current state and restarts every
// currently-enabled service. Useful as a recovery tool when the VM has drifted
// from state (e.g., a propagation failed mid-flight after a reset).
//
// No-op for local environments. Unlike `refresh`, SyncRemote does not rerun
// runner_before / build_services / runner_deploy — it only reconciles what
// could have changed as a result of state mutations (creds, store values,
// port allocations, enabled-service set).
func (m *Manager) SyncRemote(ctx context.Context, envName string) error {
	entry, err := m.state.GetEnvironment(ctx, envName)
	if err != nil {
		return fmt.Errorf("loading environment: %w", err)
	}
	if entry == nil {
		return fmt.Errorf("environment '%s' not found", envName)
	}
	if entry.Compute == nil || entry.Compute.Type != "ssh" {
		return nil
	}

	steps := []string{"generate_manifest", "generate_env", "generate_compose", "generate_nginx"}
	if err := m.RunSteps(ctx, envName, steps); err != nil {
		return fmt.Errorf("regenerating remote config: %w", err)
	}

	if len(entry.EnabledServices) == 0 {
		return nil
	}
	names := make([]string, len(entry.EnabledServices))
	copy(names, entry.EnabledServices)
	sort.Strings(names)

	ca := m.BuildSSHComputeAccess(entry)
	projectName := ComposeProjectName(m.config.Name, envName)
	cmd := fmt.Sprintf("docker compose -f .previewctl.compose.yaml -p %s up -d %s",
		projectName, strings.Join(names, " "))
	if _, err := ca.VerboseExec(ctx, cmd, nil); err != nil {
		return fmt.Errorf("restarting enabled services: %w", err)
	}
	return nil
}

// servicesAffectedByChange returns the set of services whose running container
// must be restarted so it observes the new values, plus whether compose config
// itself needs to be regenerated (true when any Start command references the
// changed value — the compose `command:` field needs to be rewritten).
//
// Direct matches come from scanning each service's Env map and Start command
// for "{{provisioner.<name>.*}}" and "{{store.<key>}}" references. Direct
// matches are then expanded via shared Path: services that live under the
// same directory share an env_file (e.g., apps/backend/.env.local), so a
// regenerated file for one affects all of them.
func servicesAffectedByChange(cfg *ProjectConfig, provisionerName string, changedStoreKeys []string) (bool, map[string]bool) {
	needles := []string{}
	if provisionerName != "" {
		needles = append(needles, fmt.Sprintf("provisioner.%s.", provisionerName))
	}
	for _, k := range changedStoreKeys {
		needles = append(needles, fmt.Sprintf("store.%s", k))
	}

	contains := func(s string) bool {
		for _, n := range needles {
			if strings.Contains(s, n) {
				return true
			}
		}
		return false
	}

	direct := map[string]bool{}
	needsCompose := false
	for name, svc := range cfg.Services {
		for _, v := range svc.Env {
			if contains(v) {
				direct[name] = true
				break
			}
		}
		if contains(svc.Start) {
			direct[name] = true
			needsCompose = true
		}
	}

	// Expand via shared Path — colocated services share .env.local.
	affectedPaths := map[string]bool{}
	for name := range direct {
		affectedPaths[cfg.Services[name].Path] = true
	}
	affected := map[string]bool{}
	for name := range direct {
		affected[name] = true
	}
	for name, svc := range cfg.Services {
		if affectedPaths[svc.Path] {
			affected[name] = true
		}
	}
	return needsCompose, affected
}

// diffStoreKeys returns keys whose value in after differs from before (or
// are present in only one). The returned slice is sorted for determinism.
func diffStoreKeys(before, after map[string]string) []string {
	seen := map[string]bool{}
	var changed []string
	for k, v := range after {
		if before[k] != v {
			changed = append(changed, k)
			seen[k] = true
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok && !seen[k] {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed
}
