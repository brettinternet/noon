#!/bin/sh
set -eu
if [ -n "${NOON_EVENT_FILE:-}" ]; then
  jq -c '.data' "$NOON_EVENT_FILE"
else
  printf '%s\n' 'No event supplied; queue this job through the remote API or MCP.'
fi
