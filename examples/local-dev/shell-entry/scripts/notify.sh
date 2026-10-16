#!/bin/sh
# on_failure hook: show the job's output as a desktop notification.
body=$(tail -n 3 "${NOON_LOG_FILE:-/dev/null}" 2>/dev/null | grep '^->' | head -n 2)
[ -n "$body" ] || body="$NOON_JOB: $NOON_STATUS"
project=$(basename "$NOON_PROJECT")
if command -v osascript >/dev/null 2>&1; then
  osascript -e 'on run argv' -e 'display notification (item 2 of argv) with title (item 1 of argv)' -e 'end run' "$project" "$body"
elif command -v notify-send >/dev/null 2>&1; then
  notify-send "$project" "$body"
fi
