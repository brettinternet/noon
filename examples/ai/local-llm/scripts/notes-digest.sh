#!/bin/sh
# Summarize Markdown notes modified in the last 7 days with a local model.
set -eu
: "${NOTES_DIR:?set NOTES_DIR}" "${MODEL:=llama3.2}"
digest_dir="$NOTES_DIR/digests"
mkdir -p "$digest_dir"
out="$digest_dir/week-$(date +%G-W%V).md"

files=$(find "$NOTES_DIR" -name '*.md' -type f -mtime -7 ! -path "$digest_dir/*" | sort)
[ -n "$files" ] || { echo "no notes changed this week"; exit 78; }

{
  printf 'Summarize this week of my notes. Sections: Themes, Decisions, Open questions, Follow-ups.\n'
  printf 'Bullets only. Cite the note file name in parentheses after each bullet.\n\n'
  printf '%s\n' "$files" | while IFS= read -r file; do
    printf '=== %s ===\n' "${file#"$NOTES_DIR"/}"
    head -c 8000 "$file"
    printf '\n\n'
  done
} | ollama run "$MODEL" > "$out.partial"
mv "$out.partial" "$out"
printf 'wrote %s from %s notes\n' "$out" "$(printf '%s\n' "$files" | wc -l | tr -d ' ')"
