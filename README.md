# previewctl

A CLI tool for managing isolated, reproducible development environments. Spin up complete sandboxes — git worktrees, Docker infrastructure, cloned databases, and auto-generated config — locally or on remote VMs.

## Features

- **Local & remote modes** — Git worktrees for local dev, SSH-provisioned VMs for CI/preview
- **Manifest-driven** — `.previewctl.json` is the single source of truth, written at provision time
- **Database cloning** — Template-based PostgreSQL cloning for fast, isolated copies
- **Deterministic port allocation** — Unique, conflict-free ports per environment (FNV-1a hash)
- **Auto-generated `.env` files** — Template variables for ports, database URLs, and more
- **Lifecycle hooks** — Run custom scripts at every stage of provisioning and running
- **Step-level checkpointing** — Each step is persisted; re-runs skip completed steps automatically
- **Audit log** — Full history of what ran, when, where, and why it failed
- **Resume from failure** — Idempotent by default; `--from <step>` to re-run from a specific point
- **Persistent state** — Track and manage multiple concurrent environments

## Install

### From source

```bash
make build
# Binary at ./bin/previewctl
```

### With Go

```bash
go install github.com/jake-landersweb/previewctl/src/cmd/previewctl@latest
```

## Quick start

1. Create a `previewctl.yaml` in your project root (see [Configuration](#configuration))
2. Validate your config:
   ```bash
   previewctl vet
   ```
3. Initialize your template database:
   ```bash
   previewctl provisioner postgres init
   ```
4. Create an environment:
   ```bash
   previewctl create my-feature --branch feat/my-feature
   ```

## Commands

### Environment lifecycle

```
previewctl create <name> [-b branch]       Create a new environment (provision + run)
previewctl attach [name] [-w path]         Attach to an existing worktree
previewctl delete [name] [-m mode]         Destroy an environment and its resources
previewctl list [--json]                   List all environments
previewctl status [name]                   Show environment details
```

### Provisioner & runner (split workflow)

For CI/remote workflows where provisioning and running happen on different machines:

```
previewctl provision <name> [-b branch] [-m mode]   Provision only (create compute, seed, write manifest)
previewctl run [--manifest path]                     Run only (install deps, env files, start infra)
```

`provision` creates the compute resources, seeds external services, and writes `.previewctl.json`. The environment is saved in "provisioned" state.

`run` reads the manifest and executes the runner phase — hooks, env file generation, docker compose, deploy.

Both support `--from <step>` to resume from a specific step and `--no-cache` to force a full re-run.

### Step inspection

```
previewctl steps [name]           Show step-by-step status (✓ completed, ✗ failed, · pending)
previewctl steps [name] --audit   Show full chronological audit log
```

### Provisioner services

```
previewctl provisioner <service> init          One-time setup (e.g., create template DB)
previewctl provisioner <service> seed [env]    Seed for a specific environment
previewctl provisioner <service> reset [env]   Reset (drop + re-seed)
previewctl provisioner <service> destroy [env] Tear down for a specific environment
```

### Maintenance

```
previewctl vet                    Validate previewctl.yaml
previewctl clean [--dry-run]      Find and remove orphaned worktrees and containers
previewctl version                Show version and check for updates
```

## Configuration

Create a `previewctl.yaml` in your project root:

**Base config** (`previewctl.yaml`):
```yaml
version: 1
name: myproject

provisioner:
  services:
    postgres:
      outputs: [CONNECTION_STRING, DB_HOST, DB_PORT]

runner:
  after: cd apps/database && pnpm migrate:run

infrastructure:
  compose_file: preview/compose.worktree.yaml

services:
  backend:
    path: apps/backend
    depends_on: [redis]
    env:
      PORT: "{{self.port}}"
      DATABASE_URL: "{{provisioner.postgres.CONNECTION_STRING}}"
      REDIS_PORT: "{{infrastructure.redis.port}}"

  web:
    path: apps/web
    depends_on: [backend]
    env:
      PORT: "{{self.port}}"
      API_URL: "http://localhost:{{services.backend.port}}"
```

**Local overlay** (`previewctl.local.yaml`):
```yaml
provisioner:
  services:
    postgres:
      init: ./scripts/import-pg-export.sh
      seed: ./scripts/seed-env.sh
      destroy: ./scripts/destroy-env-db.sh

runner:
  before: ./scripts/pre-runner.sh

infrastructure:
  compose_file: preview/compose.worktree.yaml
```

### Config overlays

Base config (`previewctl.yaml`) declares **what** — service outputs, app services, env templates.
Overlay files (`previewctl.local.yaml`, `previewctl.remote.yaml`) declare **how** — hooks, compose files, mode-specific scripts.

The tool loads `previewctl.yaml` + `previewctl.{mode}.yaml` and deep-merges them. Mode defaults to `local`; use `--mode remote` for CI workflows.

### Template variables

| Variable | Description |
|---|---|
| `{{self.port}}` | Allocated port for the current service |
| `{{services.<name>.port}}` | Allocated port for an application service |
| `{{infrastructure.<name>.port}}` | Allocated port for an infrastructure service |
| `{{provisioner.<service>.<OUTPUT>}}` | Output from a provisioner service hook |

### Hook environment variables

All hooks receive these environment variables:

| Variable | Description |
|---|---|
| `PREVIEWCTL_ENV_NAME` | Raw environment name (e.g., `feat/my-feature`) |
| `PREVIEWCTL_ENVIRONMENT_NAME` | Sanitized name safe for databases, compose, file paths (e.g., `feat_my-feature`) |
| `PREVIEWCTL_PROJECT_NAME` | Project name from config |
| `PREVIEWCTL_PROJECT_ROOT` | Absolute path to the project root |
| `PREVIEWCTL_WORKTREE_PATH` | Path to the worktree (when available) |
| `PREVIEWCTL_PORT_<NAME>` | Allocated port for each service (e.g., `PREVIEWCTL_PORT_BACKEND`) |

Provisioner service hooks also receive `PREVIEWCTL_ACTION` and `PREVIEWCTL_SERVICE_NAME`.

### Provisioner services

Provisioner services manage external resources (databases, branches, etc.) via lifecycle hooks. Declare outputs in the base config, define hooks in overlay files.

Hooks write `KEY=VALUE` pairs to stdout. Outputs are validated against the declared list and made available as template variables.

| Hook | When | Example |
|------|------|---------|
| `init` | `previewctl provisioner <name> init` | Create template DB |
| `seed` | During `previewctl create` / `provision` | Clone from template |
| `reset` | `previewctl provisioner <name> reset [env]` | Drop + re-clone |
| `destroy` | During `previewctl delete` | Drop database |

### Runner hooks

| Hook | When | Example |
|------|------|---------|
| `before` | Start of runner phase | `pnpm install`, symlinks |
| `deploy` | After infrastructure is started | Deploy services, configure proxy |
| `after` | End of runner phase | Run migrations, post PR comment |
| `destroy` | During `previewctl delete` | Cleanup before teardown |

## How it works

### Local: `previewctl create`

Runs both phases in sequence:

**Provisioner phase:**
1. `provisioner.before` hook
2. Create git worktree for code isolation
3. Allocate deterministic ports (FNV-1a hash, range 61000–65000)
4. Run provisioner service seed hooks (e.g., clone database)
5. Build manifest — resolve all template variables
6. Write `.previewctl.json` to worktree
7. `provisioner.after` hook
8. Save state with step-level checkpoints

**Runner phase:**
9. `runner.before` hook (install deps, symlinks)
10. Generate `.env` files from manifest
11. Start infrastructure via Docker Compose
12. `runner.deploy` hook
13. `runner.after` hook (run migrations)

### Remote: `previewctl provision` + `previewctl run`

**On CI** (`previewctl provision pr-42 --mode remote`):
1. `provisioner.compute.create` hook → creates VM, clones repo
2. Allocate ports, seed external services
3. Write `.previewctl.json` to VM via SSH
4. Save state

**On VM** (`previewctl run`):
1. Read `.previewctl.json`
2. `runner.before` → install deps
3. Generate `.env` files
4. `docker compose up`
5. `runner.deploy` → start services
6. `runner.after` → setup DNS, post PR comment

### Resumability

Every step is checkpointed to state immediately after completion. If an operation fails:

- **Re-run the same command** — completed steps are skipped, execution resumes from the failure point
- **`--from <step>`** — force re-execution from a specific step (invalidates all subsequent steps)
- **`--no-cache`** — skip all checkpoints, re-run everything
- **`previewctl steps <name>`** — inspect which steps completed and which failed

Stateful steps (`create_compute`, `start_infra`) verify their side effects before skipping — e.g., checking the worktree still exists or containers are still running.

### Attach mode

`previewctl attach` provisions an environment using an existing worktree (e.g., one created by Claude Code or `git worktree add`). The worktree is not managed by previewctl and will not be removed on delete — only containers are stopped.

## Manifest

The `.previewctl.json` manifest is written to the compute root during provisioning. It contains fully resolved values — no template variables. The runner reads everything it needs from this file.

```json
{
  "version": 1,
  "env_name": "pr-42",
  "project_name": "myproject",
  "branch": "feat/auth",
  "mode": "local",
  "ports": { "backend": 61200, "web": 61201, "redis": 61202 },
  "provisioner_outputs": {
    "postgres": { "CONNECTION_STRING": "postgresql://...", "DB_NAME": "wt_pr-42" }
  },
  "services": {
    "backend": {
      "path": "apps/backend",
      "env_file": ".env.local",
      "env": { "PORT": "61200", "DATABASE_URL": "postgresql://..." }
    }
  },
  "infrastructure": { "compose_file": "preview/compose.worktree.yaml" }
}
```

## License

[AGPL-3.0](LICENSE.txt)
