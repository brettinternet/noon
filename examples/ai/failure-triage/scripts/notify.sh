#!/bin/sh
# on_success hook for triage: show the newest report's one-line summary, if this run wrote one.
newest=$(ls -1t "${TRIAGE_DIR:-.noon-triage}"/*.md 2>/dev/null | head -n 1)
summary=$(head -n 1 "$newest" 2>/dev/null | cut -c 1-180)
title="noon triage: $(basename "$NOON_PROJECT")"
message=${summary:-"Diagnosis ready: noon logs $NOON_RUN_ID"}
if command -v osascript >/dev/null 2>&1; then
  osascript -e 'on run argv' -e 'display notification (item 2 of argv) with title (item 1 of argv)' -e 'end run' "$title" "$message"
elif command -v notify-send >/dev/null 2>&1; then
  notify-send "$title" "$message"
fi
