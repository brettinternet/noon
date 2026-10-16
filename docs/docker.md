# Docker

The image builds a static Go binary and runs as UID 10001. Its base images are
pinned by digest. It includes CA roots and a shell; IANA timezone data is embedded
in the binary and works without network access. No listener, scheduler, or agent
starts automatically: a bare container shows the dashboard and exits.

```sh
task docker:build
docker run --rm --read-only noon:dev --json config check
docker run --rm --read-only \
  --mount type=bind,src="$(pwd)",dst=/workspace,readonly \
  noon:dev config
```

## Production Compose

Build `noon:dev` first; no image is published yet. Copy
[`examples/compose.yaml`](../examples/compose.yaml) into your project as
`compose.yaml`, alongside an existing `noon.yaml`, then run:

```sh
docker compose up -d
docker compose logs -f
docker compose exec noon noon run JOB --detach
docker compose exec noon noon history
docker compose stop
docker compose down
```

Alternatively, keep the example in the Noon checkout and select the deployed
project explicitly:

```sh
docker compose --project-directory /absolute/path/to/project \
  -f /absolute/path/to/noon/examples/compose.yaml -p noon up -d
```

Set `NOON_IMAGE` to use your own built image. The service runs
`noon enable && exec noon serve` under Docker's init process, which forwards
signals and reaps orphaned grandchildren. It restarts unless explicitly stopped.
The root filesystem and `/workspace` project mount are read-only; `/tmp` is tmpfs.
Jobs that need writable output should use a separately mounted volume, not the
repository. Shutdown terminates active process groups and records their runs as
`interrupted`; the 15-second grace period allows Noon's 10-second kill escalation.

**Each container start approves the deployed job definitions.** Review the
repository before deploying or restarting: this entrypoint intentionally trusts
that deployment. Changes made while serving still require approval before they
run. Approval covers definitions, not the contents of referenced scripts or prompt
files. Read-only mounts prevent container writes, not changes made on the host.
Only deploy repositories you would run by hand.

## State and permissions

The image sets `NOON_STATE_DIR=/state`. The named `state` volume keeps the SQLite
store, bounded logs, approvals, and history across container recreation. A fresh
named volume inherits `/state` ownership (UID 10001) from the image. Existing
volumes or host bind mounts must already be writable by UID 10001; Docker does
not repair their ownership. Do not use volume `nocopy` for a fresh volume unless
you provision ownership yourself. Back up the volume with the scheduler stopped.
`docker compose down` keeps it; adding `--volumes` deliberately deletes history.

Keep the project mounted at the same canonical `/workspace` path across restarts.
Do not run multiple schedulers on the same state volume. Concurrent runs of the
same project job are refused by the state store.

Configuration inspection does not create state. Initialize projects on the host:
`noon init` rejects the image's `NOON_STATE_DIR` invocation override rather than
ignoring it, and cannot write a read-only project mount.

## Job tools and credentials

The image contains no agent CLI or Git toolchain. Extend it for the tools your jobs
need, returning to the non-root user afterward:

```dockerfile
FROM noon:dev
USER root
RUN apk add --no-cache git
USER noon
```

Build this as your own image and set `NOON_IMAGE` accordingly. Keep agent
credentials and the Docker socket out by default; bind only the inputs a job
actually needs. A container is not automatically safe for untrusted code, and
access to the Docker socket effectively grants control of its host.

## Verification

`task docker:smoke` builds the image and checks version/config commands with a
read-only root filesystem. `task docker:integration` requires Go, Docker Compose,
and a Linux Docker engine (including a Linux VM on macOS). It starts the actual
Compose example with a disposable project, queues a run, checks orphan reaping,
stops an active run, restarts and recreates the container, and checks history
persistence and offline non-UTC schedule previews. Resources have a unique
`noon-integration-` prefix printed by the test and are removed on completion,
including failures; cleanup asserts no prefixed containers, volumes, or images
remain. Both targets run on Linux in CI.
