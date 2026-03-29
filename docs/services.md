# Service Management

previewctl manages application services through Docker Compose, with configuration-driven builds, lifecycle commands, and reverse proxy integration.

## Service Configuration

Each service is defined under `services` in `previewctl.yaml`:

```yaml
services:
  api:
    path: ./api                    # Required. Path to service source.
    port: 8080                     # Fixed port. Optional; auto-allocated if omitted.
    build: "make build"            # Host command run before the container starts.
    start: "node server.js"        # Container command. Required for compose services.
    env:                           # Environment variables injected into the container.
      DATABASE_URL: "{{provisioner.db.url}}"
      API_KEY: "{{store.API_KEY}}"
    env_file: .env.local           # Env file loaded into the container (default: .env.local).
    proxy:                         # Reverse proxy rules (see below).
      - path: /api
        target_path: /
        to:
          service: api
    depends_on:                    # Services that must start before this one.
      - db
    command: "npm run dev"         # Override the default container command.
```

| Field        | Required | Description                                                    |
|--------------|----------|----------------------------------------------------------------|
| `path`       | Yes      | Relative path to the service source directory.                 |
| `port`       | No       | Fixed port number. Auto-allocated from a deterministic hash if omitted. |
| `build`      | No       | Command executed on the host (or remote machine) before the container starts. |
| `start`      | Yes*     | Command executed inside the Docker container. *Required for compose services. |
| `env`        | No       | Key-value map. Supports template variables.                    |
| `env_file`   | No       | Path to an env file loaded into the container. Defaults to `.env.local`. |
| `proxy`      | No       | Reverse proxy rules for nginx (see Proxy Rules below).         |
| `depends_on` | No       | List of service names that must be started first.              |
| `command`    | No       | Override container command.                                    |

## Enabled Services

When an environment is first created, the set of enabled services is seeded from `runner.compose.autostart`. This list is stored in the environment state as `EnabledServices` and is what the `build_services` and `start_services` steps operate on -- not just the autostart list.

After creation, the enabled set is modified by `env service start` and `env service stop`. This means you can enable services that were not in autostart, and disable services that were.

## Commands

All service commands require the `-e` flag to specify an environment.

### Start a Service

```bash
previewctl -e my-env env service start api
```

Builds the service (if a `build` command is configured) and runs `docker compose up` for it. Idempotent: if the service is already enabled and running, this is a no-op.

### Stop a Service

```bash
previewctl -e my-env env service stop api
```

Runs `docker compose stop` for the service and removes it from `EnabledServices`. Idempotent: if the service is already disabled, this is a no-op.

### Restart a Service

```bash
previewctl -e my-env env service restart api
```

Rebuilds the service (if a `build` command is configured) and runs `docker compose restart`.

### View Logs

```bash
previewctl -e my-env env service logs api
```

Streams Docker Compose logs for the named service. If no service name is given, logs from all services are shown.

| Flag              | Description                          |
|-------------------|--------------------------------------|
| `-f`              | Follow log output (default: off).    |
| `--tail <n>`      | Number of lines to show from the end.|
| `--since <dur>`   | Show logs since a duration (e.g. `5m`, `1h`). |
| `--until <ts>`    | Show logs until a timestamp.         |
| `-t`              | Show timestamps.                     |
| `--no-color`      | Disable colored output.              |

### List Services

```bash
previewctl -e my-env env service list
```

Displays all configured services with their current status (running, stopped, or disabled), Docker container status, and proxy URLs.

## Proxy Rules

Proxy rules allow services to be reached through the same subdomain, which is useful when authentication cookies are scoped to a single origin.

Each rule is a `ServiceProxy` entry:

```yaml
proxy:
  - path: /api            # Source path on the subdomain.
    target_path: /         # Rewritten path on the target (defaults to path).
    to:
      service: api         # Target service name.
```

These rules generate nginx `location` blocks that proxy requests from the subdomain through to the target service. For example, a request to `https://my-env--web.example.com/api/users` would be proxied to the `api` service at `/users`.

## Build vs Start

The `build` and `start` fields serve different purposes:

- **`build`** runs on the host machine (or the remote VM in remote mode) _before_ the container starts. Use this for compilation, asset generation, or any pre-container setup.
- **`start`** runs _inside_ the Docker container. This is the process the container keeps alive.

Both fields support template variables (e.g., `{{env.NAME}}`, `{{store.KEY}}`).
