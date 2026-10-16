#!/bin/sh
set -eu
: "${REPORT_DIR:?}"
out="$REPORT_DIR/changes-$(date +%F).md"
git log --since="7 days ago" --no-merges --format='- %s (%an)' |
  { printf 'Summarize this week of commits for stakeholders: themes, notable changes, risks.\n\n'; cat; } |
  claude -p --permission-prompts none > "$out"
cat "$out"
