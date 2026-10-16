#!/usr/bin/env bash
# Opt-in integration check. Never reads credentials or runs a real model.
set -euo pipefail
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

[ "$(docker info --format '{{.OSType}}')" = linux ] || fail 'Linux Docker engine required'
docker version
docker compose version
docker image inspect noon:dev noon-agent:local --format '{{.RepoTags}} {{.Id}}'

fixture=$(mktemp -d "${TMPDIR:-/tmp}/noon-docker-agent.XXXXXX")
fixture=$(cd "$fixture" && pwd -P)
project="noon-agent-smoke-$(basename "$fixture" | tr '[:upper:].' '[:lower:]-')-$$"
printf '%s\n' "$project" > "$fixture/.owner"
printf 'Fixture retained: %s (project %s)\n' "$fixture" "$project"
mkdir "$fixture/project"
# Copy only shipped inputs, not a developer's .env or private Noon config.
cp examples/integrations/docker-agent/compose.yaml "$fixture/project/"
cp -R examples/integrations/docker-agent/config examples/integrations/docker-agent/scripts "$fixture/project/"
# No schedule may dispatch a real agent, even at the scheduled weekday/time.
awk '!/^[[:space:]]*schedule:/' examples/integrations/docker-agent/noon.yaml > "$fixture/project/noon.yaml"
: > "$fixture/project/.env"
chmod 755 "$fixture" "$fixture/project"
cat > "$fixture/project/smoke.compose.yaml" <<'YAML'
services:
  noon:
    network_mode: none
YAML
compose() {
  docker compose --project-directory "$fixture/project" -p "$project" \
    -f "$fixture/project/compose.yaml" -f "$fixture/project/smoke.compose.yaml" "$@"
}
cleanup() {
  local status=$?
  trap - EXIT
  if [ "$(cat "$fixture/.owner")" != "$project" ]; then
    printf 'Ownership marker mismatch; retained project %s\n' "$project" >&2
    exit 1
  fi
  if [ "$status" != 0 ]; then compose logs --no-color >&2 || true; fi
  # Explicit unique project only: no image deletion or global prune.
  compose down --volumes --remove-orphans || status=1
  if [ -n "$(docker ps -aq --filter "label=com.docker.compose.project=$project")" ] ||
     [ -n "$(docker volume ls -q --filter "label=com.docker.compose.project=$project")" ] ||
     [ -n "$(docker network ls -q --filter "label=com.docker.compose.project=$project")" ]; then
    printf 'Owned Docker resources remain for %s\n' "$project" >&2
    status=1
  fi
  [ "$status" != 0 ] || printf 'PASS: owned project removed; no unrelated resources targeted\n'
  exit "$status"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

# Override only the entrypoint, not the image user or shipped mount options.
# This phase never enables the project or starts a scheduler.
compose run --rm --no-deps --entrypoint sh noon -ec '
  test -f /workspace/noon.yaml && test -f /config/noon/config.yaml || {
    echo "Fixture is not visible to Docker; set TMPDIR to a Docker-shared directory" >&2; exit 1;
  }
  test "$(id -u)" = 10001
  test "$HOME" = /home/noon
  claude --version; gh --version; git --version; jq --version
  grep PRETTY_NAME /etc/os-release
  claude --help | grep -q -- --permission-prompts
  for directory in /home/noon /reports /state; do
    printf writable > "$directory/smoke-write"
  done
  for directory in /workspace /config /usr/local; do
    if touch "$directory/smoke-write" 2>/dev/null; then
      echo "unexpected writable path: $directory" >&2; exit 1
    fi
  done
  noon config --json | tee /dev/stderr | jq -e '\''
    .config.timezone == "America/Chicago" and
    (.config.sources | index("/config/noon/config.yaml") != null) and
    .config.agents.claude == ["claude", "-p", "--permission-mode", "manual", "--permission-prompts", "none"]
  '\''
'
printf 'PASS: shipped profile, CLI versions, UID and Compose write boundaries\n'

# Replace only the disposable user profile and hook. The shipped prompt job,
# enable/queue/dispatch/log/history paths remain real.
cat > "$fixture/project/config/noon/config.yaml" <<'YAML'
timezone: America/Chicago
default_agent: claude
agents:
  claude:
    command: [/bin/sh, /workspace/scripts/fake-agent.sh]
on_success: [/bin/sh, /workspace/scripts/capture-hook.sh]
on_failure: [/bin/sh, /workspace/scripts/capture-hook.sh]
YAML
cat > "$fixture/project/scripts/fake-agent.sh" <<'SH'
set -eu
# Noon defaults to stdin; verify the shipped prompt arrived.
prompt=$(cat)
case "$prompt" in *'five most important maintenance risks'*) ;; *) exit 2 ;; esac
printf 'agent uid=%s\n' "$(id -u)" > /reports/agent.txt
printf 'harmless prompt completed\n'
SH
cat > "$fixture/project/scripts/capture-hook.sh" <<'SH'
set -eu
printf 'hook run=%s status=%s uid=%s\n' "$NOON_RUN_ID" "$NOON_STATUS" "$(id -u)" > /reports/hook.txt
printf 'capture hook completed\n'
SH
compose up -d --no-build
run() { compose exec -T noon "$@"; }
# Waiting is bounded; startup failures and terminal failures cannot pass.
deadline=$((SECONDS+60))
until run noon status --json > "$fixture/status.json" 2>/dev/null; do
  [ "$SECONDS" -lt "$deadline" ] || fail 'service startup timed out'
  sleep 0.2
done
run noon run repo-health --detach --json > "$fixture/queued.json"
run_id=$(run jq -er .run.id < "$fixture/queued.json")
deadline=$((SECONDS+60))
while :; do
  run noon history repo-health --json > "$fixture/history.json"
  if run jq -e --argjson id "$run_id" '
    .runs | length == 1 and .[0].id == $id and .[0].status == "succeeded" and
    .[0].exit_code == 0 and .[0].hook.status == "ok"
  ' < "$fixture/history.json" >/dev/null; then break; fi
  [ "$SECONDS" -lt "$deadline" ] || fail 'prompt job or hook did not succeed before deadline'
  sleep 0.2
done
verify_result() {
  run sh -ec 'test "$(cat /reports/agent.txt)" = "agent uid=10001"'
  run sh -ec 'test "$(cat /reports/hook.txt)" = "hook run=$1 status=succeeded uid=10001"' sh "$run_id"
  run noon logs "$run_id" --json > "$fixture/logs.json"
  run jq -e '.log | contains("harmless prompt completed")' < "$fixture/logs.json" >/dev/null
  run noon logs "$run_id" --section hook --json > "$fixture/hook-logs.json"
  run jq -e '.log | contains("capture hook completed")' < "$fixture/hook-logs.json" >/dev/null
}
verify_result
printf 'PASS: queued prompt, captured hook, successful history, log and report bytes\n'
compose restart noon
# exec may briefly fail while the init process starts after restart.
deadline=$((SECONDS+60))
until run noon history repo-health --json > "$fixture/restarted.json" 2>/dev/null; do
  [ "$SECONDS" -lt "$deadline" ] || fail 'restart timed out'
  sleep 0.2
done
cmp "$fixture/history.json" "$fixture/restarted.json"
verify_result
printf 'PASS: same-volume restart retained run history, logs and reports\n'
