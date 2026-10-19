---
id: NOON-61
title: Capture log timestamps and expose optional timestamped views
status: To Do
assignee: []
created_date: '2026-10-19 17:50'
updated_date: '2026-10-19 17:50'
labels: []
dependencies:
  - NOON-25
  - NOON-28
  - NOON-13
references:
  - 'https://docs.docker.com/reference/cli/docker/container/logs/'
  - 'https://kubernetes.io/docs/reference/kubectl/generated/kubectl_logs/'
priority: medium
type: feature
ordinal: 21000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users can diagnose when output arrived within a run without changing raw job output or breaking existing log consumers.

Context: the user approved separating timestamp capture from presentation. Docker/kubectl-style CLI log readers opt into timestamps; an inspection UI can show them by default. NOON-25 excluded timestamps from its scope; this task extends that scope while preserving its raw-byte contract.

Scope and agreed behavior:
- Always capture timing for newly recorded job and hook output, across foreground and scheduler execution. Capture means when Noon receives bytes, not when the child produced them; buffering can delay receipt.
- noon logs --timestamps / -t prefixes displayed lines; default CLI output remains unchanged whether terminal or redirected. Support bounded reads, search/context, sections, pagination, and follow. CLI timestamps use local RFC3339 time with explicit UTC offset and millisecond precision; store timing in UTC.
- TUI log views show timestamps by default and offer a discoverable toggle. Foreground noon run still forwards child stdout/stderr unchanged.
- Preserve raw log files, --path, existing log text fields, byte limits, and stable raw-byte cursors. Timing is separate metadata; JSON/MCP/HTTP expose structured timing associated with returned raw offsets, not timestamp-prefixed log strings.
- A line uses receipt time of its first byte. Partial lines must stream without waiting for newline, and resumed slices must not assign a new timestamp to the same line. Missing timing on historical or incomplete logs is explicitly unavailable, never inferred from run start or file modification time.
- Timing retention must remain bounded with retained output, track first/tail truncation and hook boundaries correctly, and be pruned with the run. Synthetic omission/hook-status markers must not masquerade as timestamped child output. Missing/incomplete metadata must not prevent raw reads.

Non-goals: changing scheduler event logging; rewriting child output; parsing application-provided timestamps; separating stdout/stderr; adding --since, global timestamp preferences, or a new logging dependency. Do not backfill historical timestamps.

Modified-file contract: internal/store/{log.go,store.go,store_test.go} and timestamp-specific files beside them; internal/runner/runner.go and runner_test.go if capture integration requires them; internal/app/{logs.go,history.go,history_test.go,hooks.go}; internal/cli/{root.go,history.go,history_test.go,run.go,run_test.go}; internal/scheduler/serve.go and its existing tests; internal/mcp/{server.go,server_test.go}; internal/httpapi/{api.go,api_test.go}; internal/tui/{tui.go,logview.go,view.go,tui_test.go}; existing shell completion files/tests if needed for the new flag; README.md, docs/{design.md,cli.md,agents.md,hooks.md}, internal/skill/SKILL.md. Justify expansions in task notes before implementation.

Dependencies: NOON-25 (bounded raw log reads), NOON-28 (hook sections), NOON-13 (TUI), all completed. One end-to-end item keeps capture semantics and readers consistent.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 mise exec -- task cli:test PKG="./internal/store ./internal/runner ./internal/app" -- -count=1 -> exit 0. Extend existing critical-path tests after implementation to verify first-byte receipt timing across split writes/partial lines, UTC persistence, bounded head/tail retention and pruning, hook offsets, and readable legacy/incomplete metadata. Raw bytes and cursors remain unchanged; no fabricated timing.
- [ ] #2 mise exec -- task cli:test PKG=./internal/cli -- -count=1 -> exit 0. Observable CLI checks cover --timestamps/-t, local offset and millisecond formatting, unchanged default and foreground output, timestamped follow without newline buffering, and raw cursor continuity across bounded/search/section reads. JSON remains raw text plus structured timing; document and test flag combinations including --path and --json.
- [ ] #3 mise exec -- task cli:test PKG="./internal/mcp ./internal/httpapi ./internal/tui" -- -count=1 -> exit 0. Structured timing maps to returned raw log offsets through MCP/HTTP without changing existing text/cursor semantics. TUI shows timestamps by default and toggles them without losing follow/search position; unavailable historical timing is honest.
- [ ] #4 mise exec -- task cli:build && mise exec -- task ci -> exit 0. Help and README/docs explain opt-in CLI timestamps, default-on TUI display and toggle, capture-versus-production time, local display/UTC storage, partial lines, structured timing schema, and historical-log behavior. Manually exercise the built CLI and TUI using isolated HOME/config/state and a local shell job with split writes; record exact commands and observed output in task notes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
