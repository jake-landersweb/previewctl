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

```yaml
version: 1
name: myproject
package_manager: pnpm  # optional

core:
  databases:
    main:
      engine: postgres
      local:
        provider: docker
        image: postgres:16
        port: 5432
        user: postgres
        password: postgres
        template_db: template_db
        seed:
          - sql: schema/seed.sql
          - run: npm run migrate

infrastructure:
  compose_file: compose.worktree.yaml

services:
  backend:
    path: apps/backend
    port: 8000
    command: pnpm dev
    depends_on: [redis]
    env:
      PORT: "{{ports.backend}}"
      DATABASE_URL: "{{databases.main}}"

  web:
    path: apps/web
    port: 3000
    depends_on: [backend]
    env:
      NEXT_PUBLIC_API_URL: "http://localhost:{{ports.backend}}"

local:
  worktree:
    symlink_patterns: [".env", ".env.*"]
  compose_file: compose.worktree.yaml

hooks:
  create:
    after:
      - run: npm run migrate
        continue_on_error: true
```

### Template variables

Use these in service `env` values:

| Variable | Description |
|---|---|
| `{{ports.<service>}}` | Allocated port for a service |
| `{{databases.<name>}}` | Connection string for a database |
| `{{env.<VAR>}}` | Value from an existing environment variable |

### Hooks

Hooks can be attached to any lifecycle step: `allocate_ports`, `create_compute`, `ensure_database`, `clone_database`, `symlink_env`, `generate_env`, `start_infra`, `save_state`, and top-level `create`/`delete`/`reset`.

Scripts receive context via environment variables: `PREVIEWCTL_ENV_NAME`, `PREVIEWCTL_BRANCH`, `PREVIEWCTL_WORKTREE_PATH`, `PREVIEWCTL_STEP`, `PREVIEWCTL_PHASE`, plus port and database info as JSON.

## How it works

When you run `previewctl create`, the tool:

1. Allocates deterministic ports (FNV-1a hash of environment name)
2. Creates a git worktree for code isolation
3. Clones databases from templates
4. Symlinks shared env files from the main worktree
5. Generates `.env.local` files with resolved template variables
6. Starts infrastructure containers via Docker Compose
7. Persists state to `~/.cache/previewctl/<project>/state.json`

`previewctl delete` reverses all of the above, cleaning up every resource.

## License

[AGPL-3.0](LICENSE.txt)
