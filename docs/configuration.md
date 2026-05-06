# Configuration Reference

## Config file discovery

previewctl searches for `previewctl.yaml` starting from the current working directory and walking up parent directories until it finds one. This means you can run previewctl from any subdirectory of your project and it will locate the config at the repository root.

## Full schema reference

### Top-level fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `version` | int | yes | Config schema version. Currently `1`. |
| `name` | string | yes | Project name. Used for state file paths and environment namespacing. |
| `provisioner` | object | no | Lifecycle hooks for compute and managed services. |
| `infrastructure` | object | no | Docker Compose infrastructure configuration. |
| `services` | map[string]object | no | Application services to manage. |
| `runner` | object | no | Deployment lifecycle hooks and compose generation config. |

### `provisioner`

The provisioner block defines hook-driven lifecycle management for compute resources and core services.

```yaml
provisioner:
  before: preview/hooks/provisioner-before.sh
  after: preview/hooks/provisioner-after.sh

  compute:
    create: preview/hooks/compute-create.sh
    destroy: preview/hooks/compute-destroy.sh
    outputs:
      - VM_NAME
      - VM_ZONE
    ssh:
      proxy_command: "compute-tunnel {{store.VM_NAME}} %p --zone={{store.VM_ZONE}}"
      user: "{{store.SSH_USER}}"
      root: /app

  services:
    database:
      outputs:
        - DATABASE_URL
        - DATABASE_NAME
      init: preview/hooks/db-init.sh
      seed: preview/hooks/db-seed.sh
      reset: preview/hooks/db-reset.sh
      destroy: preview/hooks/db-destroy.sh
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `provisioner.before` | string | — | Hook script run before provisioning starts. |
| `provisioner.after` | string | — | Hook script run after provisioning completes. |
| `provisioner.compute.create` | string | — | Hook script to create compute resources. |
| `provisioner.compute.destroy` | string | — | Hook script to destroy compute resources. |
| `provisioner.compute.outputs` | []string | — | Environment variable keys the create hook is expected to produce. |
| `provisioner.compute.ssh.proxy_command` | string | — | SSH `ProxyCommand` for tunneling into the VM. Supports `{{store.KEY}}` templates. |
| `provisioner.compute.ssh.user` | string | — | SSH username. Supports `{{store.KEY}}` templates. |
| `provisioner.compute.ssh.root` | string | `/app` | Remote working directory on the VM. Supports `{{store.KEY}}` templates. |
| `provisioner.services.<name>.outputs` | []string | **required** | Output keys the service hooks produce (stored for template resolution). |
| `provisioner.services.<name>.init` | string | — | Hook to initialize the service (first-time setup). |
| `provisioner.services.<name>.seed` | string | — | Hook to seed initial data. |
| `provisioner.services.<name>.reset` | string | — | Hook to reset the service to a clean state. |
| `provisioner.services.<name>.destroy` | string | — | Hook to tear down the service. |

Core services are managed via the CLI with `previewctl core <service> <action>`.

### `infrastructure`

Declares a Docker Compose file for infrastructure services (databases, caches, message brokers, etc.) that previewctl manages per-environment.

```yaml
infrastructure:
  compose_file: preview/compose.infrastructure.yaml
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `infrastructure.compose_file` | string | — | Path to a Docker Compose file, relative to the project root. |

Port mappings in the compose file should use the container-only form (e.g., `"5432"`) so previewctl can allocate unique host ports per environment.

Infrastructure containers can be managed independently with `previewctl infra start|stop|restart|logs`.

### `services`

Each key in the `services` map defines an application service.

```yaml
services:
  backend:
    path: apps/backend
    port: 3000
    command: npm run dev
    depends_on:
      - web
    env:
      DATABASE_URL: "postgresql://localhost:{{infrastructure.postgres.port}}/mydb"
      REDIS_URL: "redis://localhost:{{infrastructure.redis.port}}"
      SELF_PORT: "{{self.port}}"
    env_file: .env.local
    build: npm run build
    start: node dist/server.js
    proxy:
      - path: /api
        target_path: /
        to:
          service: backend
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `path` | string | **required** | Relative path from project root to the service directory. |
| `port` | int | — | Fixed port number. When set, the port allocator is skipped for this service. |
| `command` | string | — | Development command (run locally or used as the default in local mode). |
| `depends_on` | []string | — | Service names this service depends on (determines startup order). |
| `env` | map[string]string | — | Environment variables written to the env file. Values support template variables. |
| `env_file` | string | `.env.local` | Output filename for generated env vars, relative to the service `path`. |
| `build` | string | — | Build command run on the host before the container starts (remote mode). |
| `start` | string | — | Start command run inside the container. Required when using compose generation. |
| `proxy` | []object | — | Reverse proxy rules (for nginx generation in remote mode). |
| `proxy[].path` | string | — | Source URL path the proxy listens on (e.g., `/api`). |
| `proxy[].target_path` | string | Same as `path` | Path rewritten to on the target service. |
| `proxy[].to.service` | string | — | Target service name, resolved to its port at generation time. |

### `runner`

The runner block controls deployment lifecycle hooks and Docker Compose generation.

```yaml
runner:
  before: preview/hooks/runner-before.sh
  deploy: preview/hooks/deploy.sh
  destroy: preview/hooks/runner-destroy.sh
  after:
    command: preview/hooks/runner-after.sh
    allow_cache: false

  compose:
    autostart:
      - backend
      - web
    image: node:20
    proxy:
      enabled: true
      domain: preview.my-project.com
      type: nginx
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `runner.before` | string or hook object | — | Hook run before the runner phase starts. |
| `runner.build` | string or hook object | — | Global build hook run during `build_services` instead of per-service builds. |
| `runner.deploy` | string or hook object | — | Hook run to deploy the environment. |
| `runner.destroy` | string or hook object | — | Hook run to tear down runner resources. |
| `runner.after` | string or hook object | — | Hook run after the runner phase completes. |
| `runner.<hook>.command` | string | — | Command to execute when using object hook syntax. |
| `runner.<hook>.allow_cache` | bool | `true` | Whether a completed runner step checkpoint may skip this hook on later runs. Applies to `before`, `build`, `deploy`, and `after`; set `false` for hooks such as migrations that must run after each deployment. |
| `runner.compose.autostart` | []string | — | Service names started automatically on create. The proxy service is always implicit when enabled. |
| `runner.compose.image` | string | **required** | Base Docker image for application containers (e.g., `node:20`). |
| `runner.compose.proxy.enabled` | bool | `true` | Whether to generate and start a reverse proxy. |
| `runner.compose.proxy.domain` | string | — | Domain for the reverse proxy (e.g., `preview.my-project.com`). |
| `runner.compose.proxy.type` | string | `nginx` | Proxy server type. Currently only `nginx` is supported. |

## Template variables

Template variables use `{{double.brace}}` syntax and are resolved at environment creation time.

| Pattern | Example | Description |
|---------|---------|-------------|
| `{{services.<name>.port}}` | `{{services.backend.port}}` | Allocated port for an application service. |
| `{{infrastructure.<name>.port}}` | `{{infrastructure.postgres.port}}` | Allocated port for an infrastructure service. |
| `{{provisioner.<service>.<output>}}` | `{{provisioner.database.DATABASE_URL}}` | Output value from a core service hook. |
| `{{self.port}}` | `{{self.port}}` | Port of the current service (only valid inside a service `env` block). |
| `{{env.name}}` | `{{env.name}}` | Name of the current environment. |
| `{{store.<key>}}` | `{{store.VM_NAME}}` | Value from the persistent key-value store (set via `previewctl store set` or `GLOBAL_` auto-capture). |
| `{{proxy.domain}}` | `{{proxy.domain}}` | The configured proxy domain from `runner.compose.proxy.domain`. |
| `{{proxy.url.<service>}}` | `{{proxy.url.web}}` | Full URL for a service: `https://{env}--{service}.{domain}`. |

## Mode overlays

previewctl supports mode-specific configuration overlays. For a mode named `remote`, previewctl looks for `previewctl.remote.yaml` in the same directory as the base `previewctl.yaml`.

The overlay file is loaded via the `--mode` flag:

```bash
previewctl -m remote -e my-feature create
```

When `--mode` is omitted (on commands other than `create`), previewctl infers the mode from the stored environment state. On `create`, `--mode` is required.

An overlay file has the same schema as the base config. Only the fields you want to change need to be present -- everything else is inherited from the base.

Example `previewctl.remote.yaml`:

```yaml
version: 1
name: my-project

provisioner:
  compute:
    create: preview/hooks/vm-create.sh
    destroy: preview/hooks/vm-destroy.sh
    outputs:
      - VM_NAME
      - VM_ZONE
    ssh:
      proxy_command: "compute-tunnel {{store.VM_NAME}} %p --zone={{store.VM_ZONE}}"
      user: deploy

  services:
    database:
      outputs:
        - DATABASE_URL
      init: preview/hooks/db-init.sh
      destroy: preview/hooks/db-destroy.sh

runner:
  compose:
    autostart:
      - backend
      - web
    image: node:20
    proxy:
      domain: preview.my-project.com
```

## Deep merge rules

When an overlay is loaded, it is deep-merged into the base configuration. The merge behavior varies by field:

| Field | Merge behavior |
|-------|---------------|
| `version`, `name` | Overwrite if set in overlay. |
| `provisioner.before` / `provisioner.after` | Overwrite if set in overlay. |
| `provisioner.compute` | Field-level merge: `create`, `destroy`, `outputs` overwrite individually. `ssh` replaces entirely if present in overlay. |
| `provisioner.services` | Merge by key. New keys are added. Existing keys: hook fields (`init`, `seed`, `reset`, `destroy`) overwrite individually, `outputs` replaces entirely. |
| `infrastructure` | Replace entirely if present in overlay. |
| `services` | Merge by key. New keys are added. Existing keys: scalar fields (`path`, `port`, `command`, `env_file`, `build`, `start`) overwrite individually. `depends_on` and `proxy` replace entirely. `env` maps are **additive** -- overlay keys are added to or overwrite base keys, but base-only keys are preserved. |
| `runner` | Field-level merge: hook strings (`before`, `deploy`, `destroy`, `after`) overwrite individually. `compose` replaces entirely if present in overlay. |

## Env file loading

previewctl loads environment variables from files in the following order, with later sources taking priority:

1. `.env` -- loaded silently if present.
2. `.env.previewctl` -- loaded silently if present.
3. `--env-file <path>` -- one or more comma-separated file paths. These files must exist or previewctl exits with an error.

Environment variables already set in the shell (e.g., `MY_VAR=value previewctl ...`) take the highest priority and are never overwritten by any file.

These loaded variables are available to hook scripts and influence configuration, but they are separate from the per-service `env` maps that previewctl generates into `.env.local` files.
