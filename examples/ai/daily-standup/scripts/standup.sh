#!/bin/sh
# Gather facts deterministically, then let the model only summarize them.
set -eu
: "${REPOS:?space-separated repository paths}" "${STANDUP_DIR:?output directory}"
agent=${AGENT_CMD:-claude -p --permission-prompts none}

# Since the last successful standup (covers weekends and missed days), else 24h.
since=${NOON_LAST_SUCCESS_AT:-}
[ -n "$since" ] || since="24 hours ago"
author=$(git config --global user.email)

facts=$(
  for repo in $REPOS; do
    commits=$(git -C "$repo" log --all --no-merges --since="$since" --author="$author" \
      --format='- %s (%h, %ar)' 2>/dev/null || true)
    [ -z "$commits" ] || printf '## Commits in %s\n%s\n\n' "$(basename "$repo")" "$commits"
  done
  if command -v gh >/dev/null 2>&1; then
    printf '## My open PRs\n'
    gh search prs --author=@me --state=open --json repository,title,url \
      --template '{{range .}}- {{.repository.nameWithOwner}}: {{.title}} {{.url}}{{"\n"}}{{end}}' || true
    printf '\n## Reviews requested from me\n'
    gh search prs --review-requested=@me --state=open --json repository,title,url \
      --template '{{range .}}- {{.repository.nameWithOwner}}: {{.title}} {{.url}}{{"\n"}}{{end}}' || true
  fi
)

mkdir -p "$STANDUP_DIR"
note="$STANDUP_DIR/$(date +%F).md"
{
  printf 'Write my standup update from the facts below, since %s.\n' "$since"
  printf 'Sections: Yesterday, Today, Blockers. Bullets, past tense for Yesterday,\n'
  printf 'group related commits into one bullet, no commit hashes. Infer Today from open PRs\n'
  printf 'and review requests. Write "none" for Blockers unless the facts show one.\n'
  printf 'Use only these facts; do not invent work.\n\n'
  printf '%s\n' "$facts"
} | $agent > "$note.partial"
mv "$note.partial" "$note"
cat "$note"
