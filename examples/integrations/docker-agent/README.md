# Agent jobs in Docker

**Level:** advanced · **Shows:** extending the image with agent tools, user config via `XDG_CONFIG_HOME`, secrets via `env_file`, read-only project with a writable reports volume

Run scheduled agent work on an always-on box instead of your laptop. Builds on
[`examples/compose.yaml`](../../compose.yaml) and [Docker](../../../docs/docker.md).

```sh
(cd /path/to/noon && task docker:build)          # base image noon:dev
cp -R examples/integrations/docker-agent/. ~/deploy/my-repo/   # next to the repo's files
cd ~/deploy/my-repo
touch .env && chmod 600 .env                   # empty is valid; startup needs no credentials
# For real agent/notification jobs, edit .env using env.example as a guide.
docker compose up -d --build
docker compose exec noon noon status
docker compose exec noon noon run weekly-changes --detach
docker compose exec noon noon logs weekly-changes
```

| Path in container | Source | Mode |
| --- | --- | --- |
| `/workspace` | the repository | read-only |
| `/config/noon/config.yaml` | [`config/noon/config.yaml`](config/noon/config.yaml): agent profiles, failure hook | read-only |
| `/reports` | `reports` volume | writable job output |
| `/state` | `state` volume | Noon database and logs |
| `/home/noon`, `/tmp` | tmpfs | scratch, lost on restart |

## Credential-free verification

From the Noon checkout, run `task examples:docker-agent`. Requires Bash, Docker Compose v2+
(tested with v5.6.0), a Linux Docker engine, and network access for image installation. Missing
prerequisites fail the command; this opt-in check is not part of `task ci`.
The fixture directory must be shared with the Docker engine. With Colima's default home-only
mount, use `mkdir -p "$HOME/.cache/noon-smoke"` then
`TMPDIR="$HOME/.cache/noon-smoke" task examples:docker-agent`.

The target builds both images, prints versions, and copies only shipped inputs into a disposable
fixture with an empty `.env`. It removes schedules before starting anything, checks the shipped
profile and read-only mounts as UID 10001, then substitutes a local fake agent and capture-only
hook. Networking is disabled during checks. A real Noon prompt job is enabled, queued, dispatched,
and checked through history, logs, and reports, including after a same-volume restart.
Only the unique Compose project's containers and volumes are removed, even on failure.
The small host fixture is retained at the printed path for diagnosis.

The image pins Claude Code 2.1.302 using its [native Alpine installer](https://code.claude.com/docs/en/setup),
with Bash, libgcc, libstdc++, and system ripgrep. Rebuild to update the CLI; auto-updates are disabled.
The installed binary lives outside the throwaway home. This verifies installation and local
dispatch, not model authentication or inference. Real scheduled jobs need appropriate credentials;
an empty `.env` only permits credential-free startup.

Decisions worth copying:

- Agent profiles and hook defaults are user config, so they live in the mounted config directory,
  not in the repository's `noon.yaml`.
- Credentials arrive through `.env` and are visible to every job and hook; keep the set minimal
  and scope tokens (a fine-grained `GH_TOKEN` with read access only).
- The read-only root filesystem and project mount mean an agent can only write to `/reports`,
  `/tmp`, and its tmpfs home. That limits damage, but a container with network access and API
  keys is not a sandbox for untrusted repositories.
- Fresh reports/state volumes inherit UID 10001 ownership from the image. Existing volumes
  must already be writable by UID 10001. Home tmpfs explicitly sets that UID/GID and mode 0700.
- The Claude profile selects manual permission mode and denies unresolved permission prompts.
  This is not a read-only tool policy: configured permissions can still allow writes. The
  container mounts enforce the project write boundary.
- Never mount the Docker socket into this container.
