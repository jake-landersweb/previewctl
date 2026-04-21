# CLI Reference

Complete command reference for the previewctl CLI.

## Global Flags

| Flag                 | Description                                                         |
|----------------------|---------------------------------------------------------------------|
| `-m, --mode string`  | Deployment mode (`local`, `remote`). Inferred from state when omitted. |
| `-e, --env string`   | Environment name. Required for remote mode; inferred from cwd for local. |
| `--env-file string`  | Comma-separated list of env files to load.                          |
| `--ci`               | Disable colors, spinners, and animations for non-interactive environments. |
| `-v, --verbose`      | Show detailed output from internal operations.                      |

---

## Environment Lifecycle

### `create`

Create a new environment.

```bash
previewctl -m local -e my-env create -b feat/auth
previewctl -m remote -e pr-42 create -b feat/auth --base main
previewctl -e my-env create --dry-run
```

| Flag             | Description                                              |
|------------------|----------------------------------------------------------|
| `-m, --mode`     | Required on create (no state to infer from).             |
| `-b, --branch`   | Git branch to check out. Defaults to environment name.   |
| `--base`         | Base branch for new branch creation.                     |
| `--no-cache`     | Ignore step cache and re-run all steps.                  |
| `-w, --worktree` | Attach to an existing worktree instead of creating one.  |
| `--dry-run`      | Show execution plan without creating anything.           |

### `delete`

Delete an environment and clean up its resources.

```bash
previewctl -e my-env delete
```

### `list`

List all environments across state backends.

```bash
previewctl list
previewctl list --json
```

| Flag     | Description                    |
|----------|--------------------------------|
| `--json` | Output as JSON.                |

### `status`

Show the current status of an environment.

```bash
previewctl -e my-env status
previewctl -e my-env status --format markdown
```

| Flag       | Description                                |
|------------|--------------------------------------------|
| `--format` | Output format (`pretty`, `markdown`).      |

### `refresh`

Re-run runner steps after config or code changes. Works in both local and remote modes.

```bash
previewctl refresh                           # re-run all steps
previewctl refresh --only generate_env       # just regenerate env files
previewctl refresh --from build_services     # rebuild and restart onward
```

By default, all runner steps are re-run without caching. In local mode, `sync_code` is automatically skipped.

| Flag             | Description                                    |
|------------------|------------------------------------------------|
| `--only <steps>` | Run only these steps (comma-separated).        |
| `--from <step>`  | Re-run from this step onward.                  |

### `step`

Run a single runner-phase step in isolation. Works in both local and remote modes.

```bash
previewctl -e my-env step generate_env
previewctl -e my-env step generate_nginx --dry-run
previewctl -e my-env step generate_nginx --print
```

| Flag        | Description                                          |
|-------------|------------------------------------------------------|
| `--dry-run` | Show diff of current vs generated output.            |
| `--print`   | Dump full generated content to stdout.               |

Available steps: `sync_code`, `generate_manifest`, `runner_before`, `generate_env`, `start_infra`, `generate_compose`, `generate_nginx`, `build_services`, `start_services`, `runner_deploy`, `runner_after`.

### `steps`

Show step completion status for an environment.

```bash
previewctl -e my-env steps
previewctl -e my-env steps --audit
```

| Flag      | Description                              |
|-----------|------------------------------------------|
| `--audit` | Show full audit log history for all steps. |

---

## Remote-Only Commands

### `ssh`

Open an SSH session to a remote environment.

```bash
previewctl -e pr-42 ssh
```

### `service start|stop|restart`

Manage app services in a remote environment.

```bash
previewctl -e pr-42 service start api
previewctl -e pr-42 service stop api
previewctl -e pr-42 service restart api
```

### `service logs`

Stream Docker Compose logs for app services.

```bash
previewctl -e pr-42 service logs api
previewctl -e pr-42 service logs -f --tail 100
```

| Flag              | Description                                  |
|-------------------|----------------------------------------------|
| `-f`              | Follow log output.                           |
| `--tail <n>`      | Number of lines to show from the end.        |
| `--since <dur>`   | Show logs since a duration (e.g., `5m`).     |
| `--until <ts>`    | Show logs until a timestamp.                 |
| `-t`              | Show timestamps.                             |
| `--no-color`      | Disable colored output.                      |

### `service list`

Show all services with status, Docker state, and proxy URLs.

```bash
previewctl -e pr-42 service list
```

---

## Core Services

Manage core services (databases, caches, etc.) defined in `provisioner.services`. Works in both modes.

```bash
previewctl core postgres init
previewctl -e my-env core postgres seed
previewctl -e my-env core postgres reset
previewctl -e my-env core postgres destroy
```

| Subcommand | Description                                           |
|------------|-------------------------------------------------------|
| `init`     | Run one-time initialization.                          |
| `seed`     | Seed the service for an environment.                  |
| `reset`    | Reset the service to a clean state.                   |
| `destroy`  | Tear down the service for an environment.             |

---

## Infrastructure

Manage infrastructure containers (from `infrastructure.compose_file`). Works in both modes.

```bash
previewctl -e my-env infra start
previewctl -e my-env infra stop
previewctl -e my-env infra restart redis
previewctl -e my-env infra logs -f
previewctl -e my-env infra logs redis --tail 50
```

| Subcommand  | Description                                         |
|-------------|-----------------------------------------------------|
| `start`     | Start infrastructure containers.                    |
| `stop`      | Stop infrastructure containers.                     |
| `restart`   | Restart infrastructure containers.                  |
| `logs`      | View infrastructure container logs.                 |

Log flags are the same as `service logs` (`-f`, `--tail`, `--since`, `--until`, `-t`, `--no-color`).

---

## Store

Manage the persistent key-value store for an environment.

```bash
previewctl -e my-env store set KEY=VALUE OTHER=VALUE2
previewctl -e my-env store get KEY
previewctl -e my-env store list
```

Values can also be auto-captured from hook scripts by outputting `GLOBAL_KEY=VALUE` to stdout. See [Hooks](hooks.md) for details.

---

## Advanced: Split Phase Execution

For CI/remote workflows where provisioning and running happen on different machines.

### `run provision`

Run the provisioner phase only.

```bash
previewctl -e pr-42 run provision -b feat/auth
previewctl -e pr-42 run provision --from allocate_ports --no-cache
```

| Flag             | Description                                    |
|------------------|------------------------------------------------|
| `-b, --branch`   | Git branch.                                    |
| `--base`         | Base branch.                                   |
| `--from <step>`  | Start from a specific step.                    |
| `--no-cache`     | Ignore step cache.                             |

### `run runner`

Run the runner phase from an existing manifest.

```bash
previewctl run runner
previewctl run runner --manifest path/to/.previewctl.json
previewctl run runner --from start_services
```

| Flag               | Description                                 |
|--------------------|---------------------------------------------|
| `--manifest <path>` | Path to .previewctl.json (default: cwd).   |
| `--from <step>`    | Start from a specific step.                 |
| `--no-cache`       | Ignore step cache.                          |

---

## Utilities

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
