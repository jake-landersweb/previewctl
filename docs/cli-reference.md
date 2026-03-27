# CLI Reference

Complete command reference for the previewctl CLI.

## Global Flags

| Flag                 | Description                                                         |
|----------------------|---------------------------------------------------------------------|
| `-m, --mode string`  | Deployment mode (`local`, `remote`). Inferred from state when omitted. |
| `-e, --env string`   | Environment name. Required for remote mode; inferred from cwd for local. |
| `--env-file string`  | Comma-separated list of env files to load.                          |

---

## Top-Level Commands

### `vet`

Validate the previewctl.yaml configuration.

```bash
previewctl vet
```

### `clean`

Find and remove orphaned worktrees and Docker Compose projects.

```bash
previewctl clean --dry-run   # Preview what would be removed
previewctl clean             # Remove orphaned resources
```

| Flag        | Description                        |
|-------------|------------------------------------|
| `--dry-run` | Show what would be removed without acting. |

### `migrate`

Run Postgres state database migrations. Requires `PREVIEWCTL_STATE_DSN`.

```bash
previewctl migrate
```

### `version`

Print the current version and check for updates.

```bash
previewctl version
```

---

## Environment Commands

All `env` commands operate on a specific environment. Use `-e` to specify the environment name.

### `env create`

Create a new preview environment.

```bash
previewctl -e my-env env create -b feat/auth
previewctl -e my-env env create -b feat/auth --base main
previewctl -e my-env env create --dry-run
```

| Flag             | Description                                              |
|------------------|----------------------------------------------------------|
| `-b, --branch`   | Git branch to check out.                                 |
| `--base`         | Base branch for the worktree.                            |
| `--no-cache`     | Ignore step cache and re-run all steps.                  |
| `-w, --worktree` | Path to an existing worktree to use.                     |
| `--dry-run`      | Show execution plan without creating anything.           |

### `env delete`

Delete an environment and clean up its resources.

```bash
previewctl -e my-env env delete
```

### `env list`

List all environments across state backends.

```bash
previewctl env list
previewctl env list --json
```

| Flag     | Description                    |
|----------|--------------------------------|
| `--json` | Output as JSON.                |

### `env status`

Show the current status of an environment.

```bash
previewctl -e my-env env status
```

### `env ssh`

Open an SSH session to the remote environment.

```bash
previewctl -e my-env env ssh
```

### `env steps`

Show step completion status for an environment.

```bash
previewctl -e my-env env steps
previewctl -e my-env env steps --audit
```

| Flag      | Description                              |
|-----------|------------------------------------------|
| `--audit` | Show full audit log history for all steps. |

### `env reconcile`

Verify and repair environment state by re-running broken steps.

```bash
previewctl -e my-env env reconcile
previewctl -e my-env env reconcile --dry-run
```

| Flag        | Description                             |
|-------------|-----------------------------------------|
| `--dry-run` | Check health without making changes.    |

---

## Store Commands

Manage the persistent key-value store for an environment.

### `env store set`

```bash
previewctl -e my-env env store set KEY=VALUE OTHER=VALUE2
```

### `env store get`

```bash
previewctl -e my-env env store get KEY
```

### `env store list`

```bash
previewctl -e my-env env store list
```

---

## Service Commands

Manage individual services within an environment. All require `-e`.

### `env service start`

Build (if configured) and start a service.

```bash
previewctl -e my-env env service start api
```

### `env service stop`

Stop a running service.

```bash
previewctl -e my-env env service stop api
```

### `env service restart`

Rebuild and restart a service.

```bash
previewctl -e my-env env service restart api
```

### `env service logs`

Stream Docker Compose logs for a service (or all services if name is omitted).

```bash
previewctl -e my-env env service logs api
previewctl -e my-env env service logs -f --tail 100
```

| Flag              | Description                                  |
|-------------------|----------------------------------------------|
| `-f`              | Follow log output (default: off).            |
| `--tail <n>`      | Number of lines to show from the end.        |
| `--since <dur>`   | Show logs since a duration (e.g., `5m`).     |
| `--until <ts>`    | Show logs until a timestamp.                 |
| `-t`              | Show timestamps.                             |
| `--no-color`      | Disable colored output.                      |

### `env service list`

Show all services with status, Docker state, and proxy URLs.

```bash
previewctl -e my-env env service list
```

---

## Runner Commands

Low-level commands for running provisioning and runner steps directly.

### `env run provision`

Run the provisioning pipeline.

```bash
previewctl -e my-env env run provision -b feat/auth
previewctl -e my-env env run provision --from allocate_ports --no-cache
```

| Flag             | Description                                    |
|------------------|------------------------------------------------|
| `-b, --branch`   | Git branch.                                    |
| `--base`         | Base branch.                                   |
| `--from <step>`  | Start from a specific step.                    |
| `--no-cache`     | Ignore step cache.                             |

### `env run step`

Run a single named step.

```bash
previewctl -e my-env env run step generate_nginx
previewctl -e my-env env run step generate_nginx --dry-run
previewctl -e my-env env run step generate_nginx --print
```

| Flag        | Description                                          |
|-------------|------------------------------------------------------|
| `--dry-run` | Show diff of current vs generated output.            |
| `--print`   | Dump full generated content to stdout.               |

### `env run runner`

Run the runner pipeline.

```bash
previewctl -e my-env env run runner
previewctl -e my-env env run runner --manifest path/to/manifest.yaml
previewctl -e my-env env run runner --from start_services
```

| Flag               | Description                                 |
|--------------------|---------------------------------------------|
| `--manifest <path>` | Path to a custom runner manifest.           |
| `--from <step>`    | Start from a specific step.                 |
| `--no-cache`       | Ignore step cache.                          |

---

## Provisioner Commands

Run individual provisioner lifecycle actions for external services.

```bash
previewctl -e my-env env provisioner db init
previewctl -e my-env env provisioner db seed
previewctl -e my-env env provisioner db reset
previewctl -e my-env env provisioner db destroy
```

| Subcommand | Description                                           |
|------------|-------------------------------------------------------|
| `init`     | Initialize the external service.                      |
| `seed`     | Seed the service with initial data.                   |
| `reset`    | Reset the service to a clean state.                   |
| `destroy`  | Tear down the external service.                       |
