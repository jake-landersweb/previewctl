# Getting Started

## What is previewctl?

previewctl manages isolated development and preview environments for your project. It uses git worktrees for local environments and hooks into VMs for remote ones, giving each branch a fully independent copy of your codebase with its own infrastructure. Configuration is driven by a single YAML file that declares your services, infrastructure (via Docker Compose), environment variables with template resolution, and automatic port allocation so nothing collides.

## Install

```bash
go install github.com/jake-landersweb/previewctl/src/cmd/previewctl@latest
```

Verify the installation:

```bash
previewctl version
```

## Project setup

Create a `previewctl.yaml` file at the root of your repository. Here is a minimal example for a project with a backend API and a web frontend, backed by Postgres and Redis:

```yaml
version: 1
name: my-project

infrastructure:
  compose_file: preview/compose.infrastructure.yaml

services:
  backend:
    path: apps/backend
    command: npm run dev
    env:
      DATABASE_URL: "postgresql://postgres:postgres@localhost:{{infrastructure.postgres.port}}/mydb"
      REDIS_URL: "redis://localhost:{{infrastructure.redis.port}}"

  web:
    path: apps/web
    command: npm run dev
    env:
      API_URL: "http://localhost:{{services.backend.port}}"
```

The `{{infrastructure.<name>.port}}` and `{{services.<name>.port}}` templates are resolved at create time to the dynamically allocated ports for each environment.

### Infrastructure compose file

Create `preview/compose.infrastructure.yaml` with the infrastructure services referenced in your config:

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: mydb
    ports:
      - "5432"

  redis:
    image: redis:7
    ports:
      - "6379"
```

Port mappings in the compose file use the container-only form (`"5432"` rather than `"5432:5432"`). previewctl rewrites the host ports at runtime so each environment gets its own unique allocation.

## Validate

Run `vet` to check your configuration for errors before creating anything:

```bash
previewctl vet
```

This validates the YAML structure, checks that referenced files and paths exist, verifies Docker is running, and summarizes your declared services, infrastructure, and environment variables.

## Create your first environment

```bash
previewctl -e my-feature env create --branch my-feature
```

Here is what happens when you run this command:

1. **Worktree** -- previewctl creates a new git worktree for the `my-feature` branch under `~/.cache/previewctl/my-project/worktrees/my-feature/`.
2. **Port allocation** -- unique ports are allocated from the range 61000-65000 for every infrastructure and application service.
3. **Infrastructure** -- Docker Compose starts Postgres and Redis with the allocated ports.
4. **Env files** -- `.env.local` files are generated inside each service's directory with all template variables resolved to their concrete values.
5. **State** -- the environment is recorded in local state so subsequent commands can find it.

The `--branch` flag defaults to the environment name if omitted, so `previewctl -e my-feature env create` works the same way when the branch name matches.

You can also attach to an existing worktree (one created manually or by another tool) instead of letting previewctl manage it:

```bash
previewctl -e my-feature env create --worktree /path/to/existing/worktree
```

Use `--dry-run` to preview what would happen without making any changes:

```bash
previewctl -e my-feature env create --dry-run
```

## Inspect

View the current state of an environment:

```bash
previewctl -e my-feature env status
```

This shows the branch, mode (local or remote), overall status, whether infrastructure is running, provisioner outputs, and allocated ports for each service.

For a step-by-step breakdown of which provisioner and runner phases have completed, failed, or are still pending:

```bash
previewctl -e my-feature env steps
```

Add `--audit` for a full chronological log of every action taken:

```bash
previewctl -e my-feature env steps --audit
```

## Update

Re-running `env create` on an existing environment is safe. previewctl tracks which steps have already completed and skips them. Only new or failed steps are executed:

```bash
previewctl -e my-feature env create
```

To force a full re-run and ignore the step cache:

```bash
previewctl -e my-feature env create --no-cache
```

## Delete

Tear down an environment completely -- stops infrastructure, removes the worktree, and cleans up state:

```bash
previewctl -e my-feature env delete
```

If the worktree was attached (created externally via `--worktree`), it is left in place and only the previewctl state is removed.

## List environments

See all environments tracked for the current project:

```bash
previewctl env list
```

## Next steps

- **Remote mode** -- previewctl supports provisioning VMs and deploying services remotely via hook scripts. Use `--mode remote` and a `previewctl.remote.yaml` overlay to configure remote compute, SSH access, and reverse proxy settings.
- **Configuration reference** -- see [configuration.md](configuration.md) for the full YAML schema, mode overlays, deep merge rules, and template variable reference.
- **Provisioner services** -- declare external managed services (databases, queues) with lifecycle hooks for init, seed, reset, and destroy.
- **Runner hooks** -- add before/after hooks and Docker Compose generation for remote deployments.
