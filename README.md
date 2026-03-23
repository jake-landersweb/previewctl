# previewctl

A CLI tool for managing isolated, reproducible local development environments. Spin up complete sandboxes — git worktrees, Docker infrastructure, cloned databases, and auto-generated config — with a single command.

## Features

- **Isolated compute** — Git worktrees + Docker Compose per environment
- **Database cloning** — Template-based PostgreSQL cloning for fast, isolated copies
- **Deterministic port allocation** — Unique, conflict-free ports per environment
- **Auto-generated `.env` files** — Template variables for ports, database URLs, and more
- **Lifecycle hooks** — Run custom scripts before/after any step
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
3. Seed your template database:
   ```bash
   previewctl db seed --snapshot path/to/snapshot.sql
   ```
4. Create an environment:
   ```bash
   previewctl create my-feature --branch feat/my-feature
   ```

## Usage

```
previewctl create <name> [-b branch]   Create a new environment
previewctl list [--json]               List all environments
previewctl status [name]               Show environment details
previewctl delete [name]               Destroy an environment and its resources
previewctl db seed [--snapshot path]   Seed the template database
previewctl db reset [name] [--db name] Reset an environment's database from template
previewctl vet                         Validate previewctl.yaml
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

infrastructure:
  compose_file: compose.worktree.yaml

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
  before: ./scripts/ensure-core-compose.sh
  services:
    postgres:
      init: ./scripts/init-db.sh
      seed: ./scripts/seed-env.sh
      destroy: ./scripts/destroy-env-db.sh
  after: pnpm install
```

### Template variables

| Variable | Description |
|---|---|
| `{{self.port}}` | Allocated port for the current service |
| `{{services.<name>.port}}` | Allocated port for an application service |
| `{{infrastructure.<name>.port}}` | Allocated port for an infrastructure service |
| `{{provisioner.<service>.<OUTPUT>}}` | Output from a provisioner service hook |

### Provisioner services

Provisioner services manage external resources (databases, branches, etc.) via lifecycle hooks. Declare outputs in the base config, define hooks in overlay files.

Hooks write `KEY=VALUE` pairs to stdout. Outputs are validated against the declared list and made available as template variables.

| Hook | When | Example |
|------|------|---------|
| `init` | `previewctl provisioner <name> init` | Create template DB |
| `seed` | During `previewctl create` / `attach` | Clone from template |
| `reset` | `previewctl provisioner <name> reset [env]` | Drop + re-clone |
| `destroy` | During `previewctl delete` | Drop database |

### Config overlays

Base config (`previewctl.yaml`) declares WHAT — service outputs, app services, env templates.
Overlay files (`previewctl.local.yaml`, `previewctl.remote.yaml`) declare HOW — hooks, compose files, before/after scripts.

The tool loads `previewctl.yaml` + `previewctl.{mode}.yaml` and deep-merges them.

## How it works

When you run `previewctl create`, the tool:

1. Runs `provisioner.before` hook (if defined)
2. Creates a git worktree for code isolation
3. Allocates deterministic ports (FNV-1a hash, range 61000-65000)
4. Runs provisioner service seed hooks (e.g., clone database from template)
5. Runs `provisioner.after` hook (if defined)
6. Generates `.env.local` files with resolved template variables
7. Starts infrastructure containers via Docker Compose
8. Persists state to `~/.cache/previewctl/<project>/state.json`

`previewctl attach` does the same but uses an existing worktree (e.g., one created by Claude Code).

`previewctl delete` reverses all of the above, cleaning up every resource (but leaving external worktrees intact).

## License

[AGPL-3.0](LICENSE.txt)
