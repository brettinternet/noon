#!/bin/sh
# on_success hook for triage: the report's first line is the one-sentence summary.
summary=$(head -n 1 "${NOON_LOG_FILE:-/dev/null}" 2>/dev/null | cut -c 1-180)
title="noon triage: $(basename "$NOON_PROJECT")"
message=${summary:-"Diagnosis ready: noon logs $NOON_RUN_ID"}
if command -v osascript >/dev/null 2>&1; then
  osascript -e 'on run argv' -e 'display notification (item 2 of argv) with title (item 1 of argv)' -e 'end run' "$title" "$message"
elif command -v notify-send >/dev/null 2>&1; then
  notify-send "$title" "$message"
fi
