#!/bin/sh
# Claude Code Stop hook: refuse to let the agent finish while `noon run verify` fails.
# Exit 2 blocks the stop and feeds stderr back to the agent. After MAX_ATTEMPTS blocked stops in
# one session, let it finish so an unfixable failure cannot loop forever.
set -u
max=${MAX_ATTEMPTS:-3}
command -v jq >/dev/null 2>&1 || {
  echo "verify-gate requires jq; install jq and ensure it is on the hook PATH." >&2
  exit 2
}
input=$(cat)

# Nothing changed in the working tree: nothing to verify.
if git diff --quiet HEAD 2>/dev/null && [ -z "$(git ls-files --others --exclude-standard)" ]; then
  exit 0
fi

session=$(printf '%s' "$input" | jq -er '.session_id | select(type == "string" and test("^[A-Za-z0-9_-]+$"))') || {
  echo "verify-gate requires a valid session_id in the Stop payload." >&2
  exit 2
}
counter="${TMPDIR:-/tmp}/noon-verify-gate-$session"

if output=$(noon run verify 2>&1); then
  rm -f "$counter"
  exit 0
fi

attempts=$(( $(cat "$counter" 2>/dev/null || echo 0) + 1 ))
printf '%s\n' "$attempts" > "$counter"
if [ "$attempts" -gt "$max" ]; then
  rm -f "$counter"
  printf 'verify still failing after %s attempts; stopping so a human can look.\n' "$max" >&2
  exit 0
fi

{
  printf 'The verify job failed (attempt %s of %s). Fix the cause, then finish again.\n' "$attempts" "$max"
  printf 'Last 60 lines of output:\n\n'
  printf '%s\n' "$output" | tail -n 60 | tail -c 12000
} >&2
exit 2
