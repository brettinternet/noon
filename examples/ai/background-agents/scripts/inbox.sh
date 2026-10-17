#!/bin/sh
# Take the oldest agent-inbox/*.md task, run it through agent-worktree.sh, and file it under
# done/ or failed/. One task per run keeps each run short and its log readable.
set -eu
inbox=agent-inbox
mkdir -p "$inbox/done" "$inbox/failed"
task=$(ls -1tr "$inbox"/*.md 2>/dev/null | head -n 1 || true)
[ -n "$task" ] || { echo "inbox empty"; exit 78; }

name=$(basename "$task" .md)
printf 'task %s\n' "$name"
if "$(dirname "$0")/agent-worktree.sh" "$task" "$name"; then
  mv "$task" "$inbox/done/"
else
  mv "$task" "$inbox/failed/"
  exit 1
fi
