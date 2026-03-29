# Hooks

Hooks are user-defined scripts that previewctl executes at specific points in the environment lifecycle. They allow customization of provisioning and runner behavior.

## How Hooks Execute

previewctl runs each hook via `sh -c <script-path>`. The working directory depends on the phase:

- **Provisioner hooks** run with the working directory set to the project root.
- **Runner hooks** run with the working directory set to the compute root (the worktree path).

Hooks inherit the calling process's full environment plus the injected variables described below.

## Injected Environment Variables

previewctl injects variables into every hook invocation. The available variables depend on the hook type and the current state of the environment.

### Always Available

| Variable | Value |
|----------|-------|
| `PREVIEWCTL_ENV_NAME` | Raw environment name as provided by the user |
| `PREVIEWCTL_ENVIRONMENT_NAME` | Sanitized name (lowercase alphanumeric, hyphens, underscores only) |
| `PREVIEWCTL_PROJECT_NAME` | Project name from `previewctl.yaml` |
| `PREVIEWCTL_PROJECT_ROOT` | Absolute path to the project root directory |
| `PREVIEWCTL_MODE` | `local` or `remote` |

### When Worktree Path Is Known

| Variable | Value |
|----------|-------|
| `PREVIEWCTL_WORKTREE_PATH` | Absolute path to the git worktree |

### Port Variables

One variable per allocated service. The service name is uppercased with hyphens converted to underscores.

| Variable | Value |
|----------|-------|
| `PREVIEWCTL_PORT_<NAME>` | Allocated port number |

For example, a service named `my-api` produces `PREVIEWCTL_PORT_MY_API`.

### Store Variables

One variable per key in the persistent store. The key is uppercased with hyphens converted to underscores.

| Variable | Value |
|----------|-------|
| `PREVIEWCTL_STORE_<KEY>` | Stored value |

For example, a store key `redis-url` produces `PREVIEWCTL_STORE_REDIS_URL`.

### Compute Create Hook

| Variable | Value |
|----------|-------|
| `PREVIEWCTL_BRANCH` | Target git branch |
| `PREVIEWCTL_BASE_BRANCH` | Base branch (only present when `--base` was provided) |

### Compute Destroy Hook

| Variable | Value |
|----------|-------|
| `PREVIEWCTL_VM_IP` | SSH host from stored compute info |
| `PREVIEWCTL_SSH_USER` | SSH user from stored compute info |

### Provisioner Service Hooks

| Variable | Value |
|----------|-------|
| `PREVIEWCTL_ACTION` | Hook action: `init`, `seed`, `reset`, or `destroy` |
| `PREVIEWCTL_SERVICE_NAME` | Name of the provisioner service |

## Hook Outputs

Hook stdout is parsed as `KEY=VALUE` lines. Blank lines and lines starting with `#` are skipped.

```
# This is a comment and will be ignored
DATABASE_URL=postgres://user:pass@host:5432/mydb
CACHE_URL=redis://host:6379
```

For provisioner service hooks, previewctl validates that all keys declared in the `outputs` list appear in stdout. If any declared output is missing, the hook fails with an error.

Use stderr for progress messages and debug logging. Only stdout is parsed for output values.

## Hook Types

### Provisioner Hooks

| Hook | Config path | When it runs |
|------|-------------|--------------|
| `provisioner.before` | `provisioner.before` | Before any provisioner steps |
| `provisioner.after` | `provisioner.after` | After all provisioner steps complete |

### Compute Hooks

| Hook | Config path | When it runs |
|------|-------------|--------------|
| Compute create | `provisioner.compute.create` | During the `create_compute` step |
| Compute destroy | `provisioner.compute.destroy` | During environment destruction |

### Provisioner Service Hooks

Defined per service under `provisioner.services.<name>`:

| Hook | When it runs |
|------|--------------|
| `init` | First-time setup of the external service |
| `seed` | During the provisioner `seed_*` step |
| `reset` | When resetting the service to a clean state |
| `destroy` | When tearing down the external service |

### Runner Hooks

| Hook | Config path | When it runs |
|------|-------------|--------------|
| `runner.before` | `runner.before` | Start of the runner phase |
| `runner.deploy` | `runner.deploy` | After services are started |
| `runner.after` | `runner.after` | End of the runner phase |
| `runner.destroy` | `runner.destroy` | During environment destruction |

## Patterns

### Make hooks idempotent

Hooks may be re-executed on retries or when checkpoints are invalidated. Write them so that running twice produces the same result.

```bash
#!/usr/bin/env bash
set -euo pipefail

# Install only if not already present
command -v docker >/dev/null 2>&1 || install_docker

# Create database only if it doesn't exist
psql -tc "SELECT 1 FROM pg_database WHERE datname = '$DB_NAME'" \
  | grep -q 1 || createdb "$DB_NAME"
```

### Use strict shell options

Start every hook with `set -euo pipefail` to catch errors early:

```bash
#!/usr/bin/env bash
set -euo pipefail
```

### Separate output from logging

Log progress to stderr. Write `KEY=VALUE` pairs to stdout only when the hook needs to produce outputs.

```bash
#!/usr/bin/env bash
set -euo pipefail

echo "Creating database..." >&2
# ... create database ...
echo "Database created successfully" >&2

# Output for previewctl to capture
echo "DATABASE_URL=postgres://user:pass@host:5432/${PREVIEWCTL_ENV_NAME}"
```

### Use the store for cross-hook state

When one hook produces a value that a later hook or template needs, persist it to the store:

```bash
#!/usr/bin/env bash
set -euo pipefail

VM_IP=$(create_vm)
echo "VM provisioned at $VM_IP" >&2

# Persist to store so later hooks and templates can access it
"$PCTL" -m "$PREVIEWCTL_MODE" -e "$PREVIEWCTL_ENV_NAME" env store set VM_IP="$VM_IP"
```

Later hooks receive the value as `PREVIEWCTL_STORE_VM_IP`, and config templates can reference it as `{{store.VM_IP}}`.

### Reference injected variables in scripts

Use the injected environment variables to keep hooks generic across environments:

```bash
#!/usr/bin/env bash
set -euo pipefail

DB_NAME="preview_${PREVIEWCTL_ENVIRONMENT_NAME}"
echo "Provisioning database: $DB_NAME" >&2

createdb "$DB_NAME"
echo "DATABASE_URL=postgres://localhost:5432/$DB_NAME"
```
