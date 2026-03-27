# Remote Mode

Remote mode deploys preview environments to VMs or any SSH-accessible compute. previewctl provisions the compute through hook scripts, seeds external services, then runs the application stack remotely via SSH.

## Prerequisites

1. **Postgres state store** -- set the `PREVIEWCTL_STATE_DSN` environment variable and run migrations:

   ```bash
   export PREVIEWCTL_STATE_DSN="postgres://user:pass@host:5432/previewctl"
   previewctl migrate
   ```

2. **Compute hooks** -- create and destroy hook scripts that provision and tear down the remote machine.

3. **SSH access** -- the provisioned compute must be reachable via SSH (directly or through a tunnel).

## Remote Overlay

Remote-specific configuration lives in `previewctl.remote.yaml`, which is merged on top of the base `previewctl.yaml`. A typical overlay adds compute provisioning, SSH config, and service overrides:

```yaml
# previewctl.remote.yaml
provisioner:
  compute:
    create:
      command: ./scripts/compute-create.sh
      outputs:
        - VM_NAME
        - ZONE
        - SSH_USER
        - EXTERNAL_IP
    destroy:
      command: ./scripts/compute-destroy.sh
    ssh:
      proxy_command: "cloud-cli tunnel {{store.VM_NAME}} %p --zone={{store.ZONE}}"
      user: "{{store.SSH_USER}}"
      root: /app

runner:
  compose:
    autostart:
      - web
      - api
    image: my-registry/preview-base:latest
    proxy:
      domain: previews.example.com

services:
  api:
    port: 8080
    build: "make build-linux"
    start: "node dist/server.js"
    proxy:
      - path: /api
        target_path: /
        to:
          service: api
  web:
    port: 3000
    start: "npm run start"
```

## SSH Configuration

The `provisioner.compute.ssh` block configures how previewctl connects to the remote machine:

```yaml
ssh:
  proxy_command: "cloud-cli tunnel {{store.VM_NAME}} %p --zone={{store.ZONE}}"
  user: "{{store.SSH_USER}}"
  root: /app
```

| Field           | Description                                                        |
|-----------------|--------------------------------------------------------------------|
| `proxy_command` | SSH ProxyCommand for tunneling. Supports template variables.       |
| `user`          | SSH user on the remote machine. Supports template variables.       |
| `root`          | Working directory on the remote machine where the project is synced. |

Templates are resolved from store values written by the compute create hook. This design is cloud-agnostic -- it works with any provider's tunnel or bastion mechanism.

## Proxy and Nginx

Remote environments use per-service subdomains with the pattern `{env}--{service}.{domain}`.

Nginx configuration is generated automatically based on service state:

| Service State | Behavior                                                                   |
|---------------|----------------------------------------------------------------------------|
| Enabled       | Full reverse proxy with WebSocket support and a custom 502 error page.     |
| Disabled      | 503 response with a "not started" page showing the command to start it.    |
| Unknown       | 404 response with a page showing the command to list available services.   |

Error pages are generated into `preview/error-pages/` and mounted into the nginx container.

### Example URLs

```
https://pr-42--web.previews.example.com      # Web frontend
https://pr-42--api.previews.example.com      # API service (direct)
https://pr-42--web.previews.example.com/api  # API via proxy rule on web subdomain
```

## Reconciliation

The `env reconcile` command verifies and repairs environment state:

```bash
# Check health without making changes
previewctl -e pr-42 env reconcile --dry-run

# Verify and fix broken steps
previewctl -e pr-42 env reconcile
```

Reconciliation walks all completed runner steps, runs each step's verify function to check that its side effects still exist, and re-executes any step that fails verification.

The following steps are skipped during reconciliation because they are owned by hooks or external processes: `runner_before`, `runner_deploy`, `runner_after`, `sync_code`, `build_services`.

## Dry Run and Print

Preview what previewctl will do before it does it:

```bash
# Show diff of current vs generated file for a step
previewctl -e pr-42 env run step generate_nginx --dry-run

# Dump full generated content to stdout
previewctl -e pr-42 env run step generate_nginx --print

# Show full execution plan (steps, hooks, services, URLs)
previewctl -e pr-42 env create --dry-run
```

## CI Workflow

Remote environments integrate naturally into CI pipelines. A typical pattern:

```bash
# Create environment for a pull request
previewctl -m remote -e pr-${PR_NUMBER} env create --branch ${BRANCH_NAME}

# Tear down when the PR is closed
previewctl -m remote -e pr-${PR_NUMBER} env delete
```
