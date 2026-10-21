# Docker

The image runs the same binary as UID 10001, with CA roots and a shell. A bare container prints the
dashboard and exits.

```sh
task docker:build
docker run --rm --read-only noon:dev --json config check
docker run --rm --read-only \
  --mount type=bind,src="$(pwd)",dst=/workspace,readonly \
  noon:dev config
```

## Compose

No image is published yet; build `noon:dev` first. Copy
[`examples/compose.yaml`](../examples/compose.yaml) next to your `noon.yaml` as `compose.yaml`:

```sh
docker compose up -d
docker compose logs -f
docker compose exec noon noon run JOB --detach
docker compose exec noon noon history
docker compose down
```

Or keep the file in the Noon checkout and point it at your project:

```sh
docker compose --project-directory /abs/path/to/project \
  -f /abs/path/to/noon/examples/compose.yaml -p noon up -d
```

The service runs `noon enable && exec noon serve` under Docker's init (signal forwarding, orphan
reaping) and restarts unless stopped. The root filesystem and `/workspace` are read-only and `/tmp`
is tmpfs; give jobs a separate volume for output. On stop, active runs are recorded `interrupted`
within the 15-second grace period. `NOON_IMAGE` overrides the image.

**Every container start approves the deployed job definitions.** Review the repository before
deploying or restarting. Host edits made while it runs still need approval.

## State

`NOON_STATE_DIR=/state` holds the database, logs, approvals, and history on the `state` volume.

- A fresh named volume takes UID 10001 ownership from the image. Existing volumes and bind mounts
  must already be writable by UID 10001. Don't use `nocopy` on a fresh volume.
- Back up with the scheduler stopped. `docker compose down` keeps the volume; `--volumes` deletes
  history.
- Keep the project at `/workspace` across restarts, and run one scheduler per state volume.
- Run `noon init` on the host. In the container it refuses the image's `NOON_STATE_DIR` and can't
  write the read-only mount.

## Remote API and MCP

Mount the token as a secret file so jobs never inherit it. Add to the Compose `noon` service:

```yaml
services:
  noon:
    ports:
      - "127.0.0.1:8787:8787"
    secrets:
      - noon_api_token
    entrypoint:
      - /bin/sh
      - -c
      - noon enable && exec noon serve --listen 0.0.0.0:8787 --api-token-file /run/secrets/noon_api_token

secrets:
  noon_api_token:
    file: ./secrets/noon-api-token
```

The mounted file must be readable by UID 10001. Put a TLS proxy in front for remote clients.
`noon service install` doesn't take listener flags; run `noon serve` directly or under Docker.

## Tools and credentials

The image has no Git or agent CLI. Extend it for your jobs:

```dockerfile
FROM noon:dev
USER root
RUN apk add --no-cache git \
    && git config --system --add safe.directory /workspace
USER noon
```

The checkout belongs to a host user, so Git needs `/workspace` marked safe, in the image because
home is read-only at runtime.

Keep agent credentials and the Docker socket out unless a job needs them. Socket access is host
control, and a container is not a sandbox for untrusted code.
[examples/integrations/docker-agent](../examples/integrations/docker-agent/) runs agent jobs with
mounted config and secrets.

## Verification

`task docker:smoke` builds the image and checks it with a read-only root filesystem.
`task docker:integration` needs Go, Docker Compose, and a Linux engine (a VM on macOS). It checks
queueing, orphan reaping, shutdown, restarts, persistence, and offline timezones against a
throwaway project, then removes everything prefixed `noon-integration-`. CI runs both on Linux.
