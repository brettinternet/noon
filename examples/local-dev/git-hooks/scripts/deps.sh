#!/bin/sh
# Install whatever this repository uses. Reads optional trigger context from NOON_EVENT_FILE.
set -eu
if [ -n "${NOON_EVENT_FILE:-}" ] && command -v jq >/dev/null 2>&1; then
  jq -r '"triggered by \(.data.event): \(.data.from[0:8])..\(.data.to[0:8])"' "$NOON_EVENT_FILE"
fi
[ ! -f bun.lock ] || bun install --frozen-lockfile
[ ! -f go.sum ] || go mod download
