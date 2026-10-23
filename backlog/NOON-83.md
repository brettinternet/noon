---
id: NOON-83
title: Follow logs for multiple jobs in one stream
status: draft
type: feature
priority: low
blocked_by: [NOON-61]
rank: aeb29aca6b29aca1e
refs: ['https://docs.docker.com/reference/cli/docker/compose/logs/']
created: 2026-10-19T18:59:00Z
updated: 2026-10-19T18:59:00Z
---

Outcome: users can watch several jobs at once in one terminal, like docker compose logs -f, without a merged read model that pretends to be an exact timeline.

Promotion condition: draft until NOON-61 ships and users report that separate panes or the TUI are insufficient. Revisit the decisions below against that feedback before marking it ready.

Context: multiple targets (df67a77) intentionally kept logs single-target, and NOON-25 made multi-run reads a non-goal. A merged bounded read would need composite cursors and new JSON/MCP/HTTP schemas, and could not place untimed historical lines or synthetic markers honestly. NOON-61 receipt timing is millisecond, receipt-based, and absent for older logs, so it cannot order a historical merge reliably. A live follow avoids those problems: lines are shown in the order Noon reads them.

Proposed behavior:
- noon logs A B --follow accepts two or more targets, each a job name or run ID, deduplicated as in cancel. A name selects its latest started run, as single-target logs NAME does; --status applies per name. A queued run is awaited as in single-target follow.
- Each emitted line is prefixed with its target, padded to the longest target, then a pipe: "tests | ...". With --timestamps, the NOON-61 timestamp follows the target prefix. Target prefixes are the only addition to child output.
- Output is whole lines in read order across runs. A partial line is held per run until its newline or until that run finishes, so lines never interleave mid-line; this intentionally differs from single-target follow, which streams partial lines.
- --tail N applies per run before following; without it, each run starts from its beginning.
- Following ends when every selected run is terminal, including hooks. Exit 1 if any run is orphaned, with each orphan named and the existing noon cancel hint; otherwise exit 0.
- Single-target logs behavior, output, JSON, and cursors are unchanged.

Non-goals: multi-target reads without --follow; --json, --after-cursor, --section, --match, --context, --limit-bytes, or --path with multiple targets (usage errors with a single-target hint); composite cursors; MCP/HTTP/TUI multi-run views; sorting by stored timestamps; picking up runs that start after following begins; colorizing prefixes.

Open questions to settle before ready: whether names should also follow runs that start later (compose-style) instead of exiting when the selected runs finish; whether the target prefix should be colored when stdout is a terminal under the existing NO_COLOR rules.

Modified-file contract (provisional): internal/cli/{root.go,history.go,history_test.go}; internal/app/{history.go,history_test.go}; existing shell completion files/tests if needed; README.md, docs/{cli.md,design.md}, internal/skill/SKILL.md.

Dependencies: NOON-61 (timestamp display and follow semantics).

## Acceptance criteria

- [ ] mise exec -- task cli:test PKG="./internal/app ./internal/cli" -- -count=1 -> exit 0. Tests cover target prefixes and padding, whole-line interleaving with partial lines held per run, per-run --tail, --timestamps after the prefix, waiting on a queued run, exit after all runs and hooks finish, orphan exit 1 naming the run, and usage errors for unsupported flag combinations; single-target output is unchanged.
- [ ] mise exec -- task cli:build && mise exec -- task ci -> exit 0. Help and docs/cli.md describe multi-target follow and its limits. Manually follow two local shell jobs with interleaved split writes using isolated HOME/config/state; record exact commands and observed output in task notes.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
