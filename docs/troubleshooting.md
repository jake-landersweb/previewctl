# Troubleshooting

Debugging and recovery guide for previewctl environments.

## Inspect State

Start by understanding what previewctl thinks is happening:

```bash
# High-level environment overview
previewctl -e my-env env status

# Step completion status
previewctl -e my-env env steps

# Full audit history of all actions
previewctl -e my-env env steps --audit
```

## Re-Run from a Specific Step

If a step failed or produced incorrect output, re-run from that point:

```bash
# Re-run provisioning from a specific step
previewctl -e my-env env run provision --from allocate_ports

# Full re-run ignoring all cached steps
previewctl -e my-env env create --no-cache
```

## Preview Changes

Before applying changes, inspect what previewctl would generate:

```bash
# Show diff of current vs generated file for a step
previewctl -e my-env env run step generate_nginx --dry-run

# Dump full generated content to stdout
previewctl -e my-env env run step generate_nginx --print

# Show full execution plan (steps, hooks, services, URLs)
previewctl -e my-env env create --dry-run
```

## Automated Healing

The reconcile command walks all completed runner steps, verifies their side effects still exist, and re-executes any that fail:

```bash
# Check health without making changes
previewctl -e my-env env reconcile --dry-run

# Verify and fix broken steps
previewctl -e my-env env reconcile
```

## Clean Orphaned Resources

Find and remove orphaned worktrees and Docker Compose projects that are no longer tracked by any environment:

```bash
# Preview what would be removed
previewctl clean --dry-run

# Remove orphaned resources
previewctl clean
```

## Common Issues

### Port Conflict

Two environments hash to the same port allocation. Fix by setting a fixed port in the service config:

```yaml
services:
  api:
    port: 8080
```

### Docker Not Running

previewctl requires Docker for infrastructure containers and Compose services. Ensure the Docker daemon is running:

```bash
docker info
```

### Missing Provisioner Outputs

Hook scripts must print all declared output keys as `KEY=VALUE` lines to stdout. If a hook declares outputs but does not print them, downstream steps that reference `{{provisioner.service.key}}` will fail.

```bash
# Example hook script
#!/bin/bash
set -euo pipefail
echo "DB_URL=postgres://localhost:5432/preview"
echo "DB_NAME=preview"
```

### Hook Script Failures

Check the stderr output from the failed hook. Hook scripts should use `set -euo pipefail` to fail fast on errors. Run the hook manually to debug:

```bash
# Check which step failed
previewctl -e my-env env steps

# Re-run the specific step
previewctl -e my-env env run step <step-name>
```

### Stale State

If environment state is inconsistent with reality (e.g., after a manual cleanup), the safest path is to delete and recreate:

```bash
previewctl -e my-env env delete
previewctl -e my-env env create -b my-branch
```

### Step Stuck as Completed but Broken

A step may be marked as completed in state while its actual side effects (files, containers) are missing or broken. Options:

```bash
# Force re-execution of a single step
previewctl -e my-env env run step <step-name>

# Automated fix across all steps
previewctl -e my-env env reconcile
```

### Mode Inference Fails

If previewctl cannot find the environment in any state backend, mode inference fails. Use the explicit `-m` flag:

```bash
previewctl -m remote -e my-env env status
```

## Manual State Inspection

When the CLI is not providing enough detail, inspect state directly.

### Local Mode

```bash
cat ~/.cache/previewctl/{project}/state.json | jq .
```

### Remote Mode (Postgres)

Query the `environments` table directly:

```sql
SELECT name, mode, branch, status, updated_at
FROM environments
WHERE is_deleted = false
ORDER BY updated_at DESC;
```
