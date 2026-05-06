# Environment Lifecycle

This document covers the full lifecycle of a previewctl environment from creation through destruction.

## Two-Phase Architecture

previewctl splits environment setup into two phases:

- **Provisioner phase** -- sets up WHERE the environment runs: compute resources, port allocations, and external services.
- **Runner phase** -- sets up WHAT runs in the environment: dependencies, env files, infrastructure containers, and application services.

These phases can run together with `create` or independently:

```bash
# Combined
previewctl -m local -e my-env create

# Split (CI/remote workflows)
previewctl -e my-env run provision
previewctl run runner
```

Splitting is useful when provisioning and running happen on different machines (e.g., provisioning on CI, running on a remote VM).

## Provisioner Phase

Steps execute in this order:

1. **provisioner_before** -- Optional hook. Runs before any provisioning work begins.

2. **create_compute** -- Creates the compute target. In local mode, this adds a git worktree. In remote mode, this runs the `compute.create` hook (e.g., to spin up a VM).

3. **allocate_ports** -- Assigns ports to each service deterministically using an FNV-1a hash of the environment name. Ports fall in the range 61000--65000. Services with a `port` value set explicitly in config use that fixed port instead.

4. **seed_\*** -- One step per core service that defines a `seed` hook. Each runs its seed script to set up external resources (databases, caches, etc.) and capture outputs.

5. **build_manifest** -- Resolves all template variables in `previewctl.yaml` and produces the final manifest object. See [Template Variables](template-variables.md) for details.

6. **write_manifest** -- Writes `.previewctl.json` to the compute root directory.

7. **provisioner_after** -- Optional hook. Runs after all provisioning is complete.

## Runner Phase

Steps execute in this order:

1. **sync_code** -- Fetches from origin and resets the worktree to the latest commit on the target branch. Automatically skipped in local mode (you manage your own code locally).

2. **runner_before** -- Optional hook. Typically used to install system-level dependencies.

3. **generate_env** -- Writes `.env.local` files for each service using the resolved values from the manifest.

4. **start_infra** -- Runs `docker compose up -d` for infrastructure services (databases, message queues, etc.).

5. **generate_compose** -- Writes `.previewctl.compose.yaml` when `runner.compose` is configured. This compose file defines the application services for Docker-based runners.

6. **generate_nginx** -- Writes `preview/nginx.conf` and error pages when the proxy is enabled. Configures routing from the proxy to individual services.

7. **build_services** -- Runs build commands for each service that has one configured.

8. **start_services** -- Runs `docker compose up -d` for application service containers and the proxy.

9. **runner_deploy** -- Optional hook. Runs after services are started (e.g., register with a load balancer).

10. **runner_after** -- Optional hook. Runs last (e.g., run database migrations, seed test data).

## Checkpointing

Each step's result is persisted to state immediately after it completes. On subsequent runs, completed steps are skipped.

Certain steps have verify functions that check whether their side effects still exist before skipping:

- **generate_env** -- verifies `.env.local` files are present
- **start_infra** -- verifies infrastructure containers are running
- **generate_compose** -- verifies `.previewctl.compose.yaml` exists
- **generate_nginx** -- verifies `preview/nginx.conf` exists
- **start_services** -- verifies service containers are running

If a verify check fails (e.g., a file was deleted or a container stopped), the step re-executes even though it was previously completed.

Runner hooks are cacheable by default, matching older string hook behavior. Use
object hook syntax with `allow_cache: false` for hooks that should always run
when reached, such as database migrations:

```yaml
runner:
  after:
    command: cd apps/backend && pnpm migration:run
    allow_cache: false
```

## Resumability

previewctl provides several mechanisms for controlling re-execution:

- **`refresh`** -- Re-runs all runner steps with caching disabled. Use `--only` to target specific steps or `--from` to start from a specific point.

  ```bash
  previewctl refresh
  previewctl refresh --only generate_env
  previewctl refresh --from build_services
  ```

- **`--from <step>`** -- On `run provision` or `run runner`, invalidates the specified step and all steps after it, forcing them to re-execute.

  ```bash
  previewctl run runner --from generate_env
  ```

- **`--no-cache`** -- Skips all checkpoint checks and re-executes every step.

  ```bash
  previewctl -e my-env create --no-cache
  ```

- **Failed creation** -- If `create` fails partway through, re-running the same command picks up from the failed step. There is no need to destroy and recreate.

## Audit Log

Every step decision is recorded with:

- Timestamp
- Machine identifier
- Action taken: `executed`, `skipped`, `verified`, `verify_failed`, `invalidated`, or `failed`

View the audit log with:

```bash
previewctl -e my-env steps --audit
```

## Attach Mode

Use `--worktree` to attach an environment to an existing git worktree instead of having previewctl create one:

```bash
previewctl -m local create --worktree /path/to/existing/worktree
```

In attach mode, previewctl does not manage the worktree's lifecycle. The worktree is not deleted when the environment is destroyed.

## Branch Semantics

Two flags control branch behavior during environment creation:

- **`--branch`** -- The target branch the environment uses. Defaults to the environment name if not specified.

- **`--base`** -- The branch to create the target branch from. Only applies when the target branch does not yet exist. If `--base` is provided and the target branch already exists, previewctl produces an error.

Examples:

```bash
# Creates branch "my-feature" from current HEAD
previewctl -m local -e my-feature create

# Creates branch "my-feature" from "main"
previewctl -m local -e my-feature create --base main

# Uses existing branch "release/v2" as-is
previewctl -m local -e staging create --branch release/v2
```
