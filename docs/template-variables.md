# Template Variables

previewctl supports template variables in `previewctl.yaml` env blocks and service start commands. Variables are replaced with concrete values during manifest generation.

## Syntax

All template variables use double-brace syntax:

```
{{namespace.path}}
```

Namespaces are fixed identifiers (`self`, `services`, `infrastructure`, `provisioner`, `env`, `store`, `proxy`). The path following the namespace selects a specific value.

## Variable Reference

| Variable | Resolves to | Context |
|----------|-------------|---------|
| `{{self.port}}` | The current service's allocated port | Only inside a service `env` block |
| `{{services.<name>.port}}` | A named service's allocated port | Anywhere |
| `{{infrastructure.<name>.port}}` | An infrastructure service's allocated port | Anywhere |
| `{{provisioner.<service>.<OUTPUT>}}` | A provisioner hook's output value | After the provisioner service runs |
| `{{env.name}}` | The environment name | Anywhere |
| `{{store.<KEY>}}` | A value from the persistent store | Anywhere (errors if the key is missing) |
| `{{proxy.url.<service>}}` | Full proxy URL: `https://{env}--{service}.{domain}` | Requires proxy domain configured |
| `{{proxy.domain}}` | The configured proxy domain string | Requires proxy domain configured |

## Resolution Timing

Template variables are resolved at manifest build time during the provisioner phase. The `build_manifest` step reads `previewctl.yaml`, substitutes every template variable with its concrete value, and writes the result to `.previewctl.json`. By the time the runner phase executes, all values are fully resolved in the manifest -- no template syntax remains.

This means:

- Port allocations must complete before templates resolve.
- Provisioner service hooks must finish before their outputs are available.
- Store keys must be set before any template referencing them is evaluated.

## Error Behavior

previewctl treats unresolvable variables as hard errors at build time:

- **Unknown namespace** -- `{{unknown.foo}}` produces an error.
- **Missing service** -- `{{services.nonexistent.port}}` produces an error if no service by that name is defined.
- **Unset store key** -- `{{store.MISSING_KEY}}` produces an error if the key has not been set.
- **Missing provisioner output** -- `{{provisioner.db.CONNECTION_URL}}` produces an error if the provisioner service `db` did not emit `CONNECTION_URL`.

There are no silent empty strings. If a variable cannot be resolved, the build fails with a message identifying the unresolvable variable.

## Examples

### Service referencing another service's port

A backend service that needs to know the port of an API gateway:

```yaml
services:
  gateway:
    start: ./start-gateway.sh
    env:
      PORT: "{{self.port}}"

  backend:
    start: ./start-backend.sh
    env:
      PORT: "{{self.port}}"
      GATEWAY_URL: "http://localhost:{{services.gateway.port}}"
```

### Using provisioner output for DATABASE_URL

A provisioner service creates a database and emits connection details. Downstream services consume the output:

```yaml
provisioner:
  services:
    db:
      seed: ./scripts/provision-db.sh
      outputs:
        - DATABASE_URL

services:
  api:
    start: ./start-api.sh
    env:
      DATABASE_URL: "{{provisioner.db.DATABASE_URL}}"
```

The `provision-db.sh` hook writes the output to stdout:

```bash
#!/usr/bin/env bash
set -euo pipefail
# ... create database ...
echo "DATABASE_URL=postgres://user:pass@host:5432/preview_${PREVIEWCTL_ENV_NAME}"
```

### Using proxy.url for frontend URLs

A frontend application needs to know the public URL of the API it calls through the proxy:

```yaml
services:
  web:
    start: npm run dev
    env:
      PORT: "{{self.port}}"
      NEXT_PUBLIC_API_URL: "{{proxy.url.api}}"
      NEXT_PUBLIC_APP_DOMAIN: "{{proxy.domain}}"

  api:
    start: ./start-api.sh
    env:
      PORT: "{{self.port}}"
      CORS_ORIGIN: "{{proxy.url.web}}"
```

### Using store for dynamically provisioned values

A provisioner hook sets a value in the store, and later configuration references it:

```bash
# In a provisioner hook:
"$PCTL" -m "$PREVIEWCTL_MODE" -e "$PREVIEWCTL_ENV_NAME" env store set REDIS_URL="redis://10.0.0.5:6379"
```

```yaml
services:
  worker:
    start: ./start-worker.sh
    env:
      REDIS_URL: "{{store.REDIS_URL}}"
```
