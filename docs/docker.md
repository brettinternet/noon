# Docker

The image runs the same binary as a non-root user (UID 10001). It has CA roots and a shell, and
timezone data is built in. A bare container prints the dashboard and exits; nothing starts on its
own.

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

The service runs `noon enable && exec noon serve` under Docker's init process, which forwards
signals and reaps orphaned processes. It restarts unless stopped. The root filesystem and the
`/workspace` project mount are read-only, and `/tmp` is tmpfs; give jobs a separate volume for
output. On stop, active runs are terminated and recorded as `interrupted` within the 15-second grace
period. Set `NOON_IMAGE` to use your own image.

**Every container start approves the deployed job definitions.** Review the repository before
deploying or restarting. Edits made while running still need approval. A read-only mount stops the
container from writing; it doesn't stop changes on the host.

## State

`NOON_STATE_DIR=/state` holds the database, logs, approvals, and history on the `state` volume.

- A fresh named volume takes UID 10001 ownership from the image. Existing volumes and bind mounts
  must already be writable by UID 10001. Don't use `nocopy` on a fresh volume.
- Back up with the scheduler stopped. `docker compose down` keeps the volume; `--volumes` deletes
  history.
- Keep the project at `/workspace` across restarts, and run one scheduler per state volume.
- Run `noon init` on the host. In the container it refuses the image's `NOON_STATE_DIR` and can't
  write the read-only mount.

## Tools and credentials

The image has no Git or agent CLI. Extend it for your jobs:

```dockerfile
FROM noon:dev
USER root
RUN apk add --no-cache git \
    && git config --system --add safe.directory /workspace
USER noon
```

The mounted checkout belongs to a host user, so Git needs `/workspace` marked safe. The home
directory is read-only at runtime, so set this in the image.

Keep agent credentials and the Docker socket out unless a job needs them. Socket access is control
of the host, and a container is not a sandbox for untrusted code.
[examples/integrations/docker-agent](../examples/integrations/docker-agent/) runs agent jobs with
mounted config and secrets.

## Verification

`task docker:smoke` builds the image and checks it with a read-only root filesystem.
`task docker:integration` needs Go, Docker Compose, and a Linux engine (a VM on macOS). It runs the
Compose example against a throwaway project and checks queueing, orphan reaping, shutdown,
restarts, history persistence, and offline timezones. It removes everything prefixed
`noon-integration-`, even on failure. CI runs both on Linux.
