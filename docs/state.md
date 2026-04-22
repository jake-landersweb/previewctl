# State Management

previewctl tracks environment lifecycle, configuration outputs, and audit history through a pluggable state backend.

## State Backends

### File (Local Mode)

State is stored as JSON at `~/.cache/previewctl/{project}/state.json`. Writes are atomic (temp file + rename) and mutex-protected to prevent corruption from concurrent access.

### Postgres (Remote Mode)

Requires the `PREVIEWCTL_STATE_DSN` environment variable pointing to a Postgres connection string. Before first use, run migrations:

```bash
previewctl migrate
```

Schema is managed by goose migrations. Environments are soft-deleted (`is_deleted=true`) on delete, preserving history for audit purposes.

## Environment Entry

Each environment stores the following fields:

| Field                | Description                                                   |
|----------------------|---------------------------------------------------------------|
| `name`               | Environment name (e.g., `pr-42`).                             |
| `mode`               | Deployment mode: `local` or `remote`.                         |
| `branch`             | Git branch checked out in this environment.                   |
| `status`             | Lifecycle status: `creating`, `provisioned`, `running`, `stopped`, `error`. |
| `createdAt`          | Timestamp of environment creation.                            |
| `updatedAt`          | Timestamp of last state update.                               |
| `ports`              | Map of service name to allocated port number.                 |
| `provisionerOutputs` | Map of service name to key-value outputs from core service hooks. |
| `compute`            | Compute details: type (`local`/`ssh`), host, user, path, managedWorktree, metadata. |
| `env`                | Persistent key-value store (see below).                       |
| `enabledServices`    | List of currently enabled service names.                      |
| `steps`              | Checkpoint records for each completed step.                   |
| `auditLog`           | Append-only history of actions performed on this environment. |

## Mode Inference

When `--mode` is omitted, previewctl infers the mode:

1. If `--env` is set, look up the environment in Postgres (if `PREVIEWCTL_STATE_DSN` is available), then fall back to file state.
2. Use the stored `entry.Mode` from whichever backend found it.
3. If the environment is not found in either source, return an error.
4. If `--env` is not set at all, default to `local`.

**Note:** `--mode` is required on `create` since there is no existing state to infer from.

## Persistent Store

Each environment has a key-value store for persisting arbitrary data across commands and hooks.

### Commands

```bash
# Set one or more values
previewctl -e my-env store set KEY=VALUE OTHER_KEY=OTHER_VALUE

# Get a single value
previewctl -e my-env store get KEY

# List all stored key-value pairs
previewctl -e my-env store list
```

### GLOBAL_ Auto-Capture

Hook scripts can auto-persist values to the store by outputting lines with the `GLOBAL_` prefix to stdout. For example, a hook that outputs `GLOBAL_GCP_ZONE=us-central1-a` will persist `GCP_ZONE=us-central1-a` to the environment store automatically. This eliminates the need for hook scripts to depend on the previewctl CLI binary.

### Integration with Hooks and Templates

- Stored values are injected into hook scripts as environment variables prefixed with `PREVIEWCTL_STORE_`. For example, a stored key `ZONE` is available as `PREVIEWCTL_STORE_ZONE`.
- Stored values are available in template expressions as `{{store.KEY}}`.
- Common pattern: a compute create hook writes cloud-specific state (zone, project ID, VM name) to the store via `GLOBAL_` outputs, and later hooks or config templates reference those values.

## List Command

```bash
previewctl list
```

Queries both file and Postgres state backends, deduplicates results, and displays each environment with a mode badge (`local` or `remote`).

```bash
previewctl list --json
```

Outputs the environment list as JSON for scripting.
