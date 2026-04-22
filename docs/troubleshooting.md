# Troubleshooting

Debugging and recovery guide for previewctl environments.

## Inspect State

Start by understanding what previewctl thinks is happening:

```bash
# High-level environment overview
previewctl -e my-env status

# Step completion status
previewctl -e my-env steps

# Full audit history of all actions
previewctl -e my-env steps --audit
```

## Refresh After Changes

If you changed `previewctl.yaml` or need to resync an environment:

```bash
# Re-run all runner steps
previewctl refresh

# Just regenerate env files
previewctl refresh --only generate_env

# Rebuild from a specific step onward
previewctl refresh --from build_services
```

## Re-Run a Single Step

If a specific step failed or produced incorrect output:

```bash
# Re-run one step in isolation
previewctl -e my-env step generate_nginx

# Preview what would change
previewctl -e my-env step generate_nginx --dry-run

# Dump full generated content to stdout
previewctl -e my-env step generate_nginx --print
```

## Re-Run from a Specific Step

```bash
# Re-run provisioning from a specific step
previewctl -e my-env run provision --from allocate_ports

# Full re-run ignoring all cached steps
previewctl -e my-env create --no-cache
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

### Missing Core Service Outputs

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
previewctl -e my-env steps

# Re-run the specific step
previewctl -e my-env step <step-name>
```

### Stale State

If environment state is inconsistent with reality (e.g., after a manual cleanup), the safest path is to delete and recreate:

```bash
previewctl -e my-env delete
previewctl -m local -e my-env create -b my-branch
```

### Step Stuck as Completed but Broken

A step may be marked as completed in state while its actual side effects (files, containers) are missing or broken:

```bash
# Force re-execution of a single step
previewctl -e my-env step <step-name>

# Refresh all steps
previewctl refresh
```

### Mode Inference Fails

If previewctl cannot find the environment in any state backend, mode inference fails. Use the explicit `-m` flag:

```bash
previewctl -m remote -e my-env status
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
