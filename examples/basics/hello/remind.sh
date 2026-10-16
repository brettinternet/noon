#!/bin/sh
# Show a desktop notification when possible; always print the message to the run log.
set -eu
message=${1:-Reminder}
printf '%s\n' "$message"
if command -v osascript >/dev/null 2>&1; then
  osascript -e 'on run argv' -e 'display notification (item 1 of argv) with title "noon"' -e 'end run' "$message"
elif command -v notify-send >/dev/null 2>&1; then
  notify-send noon "$message"
fi
