# Agent jobs in Docker

**Level:** advanced · **Shows:** extending the image with agent tools, user config via `XDG_CONFIG_HOME`, secrets via `env_file`, read-only project with a writable reports volume

Run scheduled agent work on an always-on box instead of your laptop. Builds on
[`examples/compose.yaml`](../../compose.yaml) and [Docker](../../../docs/docker.md).

```sh
(cd /path/to/noon && task docker:build)          # base image noon:dev
cp -R examples/integrations/docker-agent/. ~/deploy/my-repo/   # next to the repo's files
cd ~/deploy/my-repo
cp env.example .env && chmod 600 .env && $EDITOR .env
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

Decisions worth copying:

- Agent profiles and hook defaults are user config, so they live in the mounted config directory,
  not in the repository's `noon.yaml`.
- Credentials arrive through `.env` and are visible to every job and hook; keep the set minimal
  and scope tokens (a fine-grained `GH_TOKEN` with read access only).
- The read-only root filesystem and project mount mean an agent can only write to `/reports`,
  `/tmp`, and its tmpfs home. That limits damage, but a container with network access and API
  keys is not a sandbox for untrusted repositories.
- Never mount the Docker socket into this container.
