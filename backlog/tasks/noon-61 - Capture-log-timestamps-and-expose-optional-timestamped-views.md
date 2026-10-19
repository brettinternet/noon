---
id: NOON-61
title: Capture log timestamps and expose optional timestamped views
status: Done
assignee: []
created_date: '2026-10-19 17:50'
updated_date: '2026-10-19 20:04'
labels:
  - reviewed
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

Context: the user approved separating timestamp capture from presentation. Docker/kubectl-style CLI log readers opt into timestamps; an inspection UI can show them by default. NOON-25 excluded timestamps from its scope; this task extends that scope while preserving its raw-byte contract (logs stay raw combined bytes readable with cat and rg).

Scope and agreed behavior:
- Always capture timing for newly recorded job and hook output, across foreground and scheduler execution. Capture means when Noon receives bytes, not when the child produced them; buffering can delay receipt.
- noon logs --timestamps / -t prefixes displayed lines; default CLI output remains unchanged whether terminal or redirected. Support bounded reads, search/context, sections, pagination, and follow. (-t means --timeout on noon run; the alias is per-command, matching docker logs -t.)
- TUI log views show timestamps by default and offer a discoverable toggle. Foreground noon run still forwards child stdout/stderr unchanged.
- Preserve raw log files, --path, existing log text fields, byte limits, and stable raw-byte cursors. Timing is separate metadata; JSON/MCP/HTTP expose structured timing keyed by raw file offsets, never timestamp-prefixed log strings.
- A line uses receipt time of its first byte. Partial lines must stream without waiting for newline, and resumed slices must not assign a new timestamp to the same line. Missing timing on historical or incomplete logs is explicitly unavailable, never inferred from run start, neighbouring lines, or file modification time.
- Synthetic omission/hook-status markers have no timing. Missing/incomplete metadata must not prevent raw reads or fail the run.

Settled design:
- Storage: a sidecar `logs/PROJECT_ID/RUN_ID.log.timing` beside the log (decision-3: logs are files beside the database; no schema migration, no per-chunk SQLite writes). Created with the log, 0600, removed by the same retention path as the log (pruneRuns/removeLog); a missing sidecar is not an error. --path still prints only the log path.
- Format: append-only fixed-width little-endian records (start offset, end offset, receipt unix ms), each covering one received chunk that contains at least one line start, from its first line start to the chunk end. Adjacent records with equal ms and contiguous coverage may be coalesced. A line takes the time of the record covering its first byte; an uncovered line start is unavailable. This bounds records by retained line starts and keeps uncovered (synthetic, lost, or never-recorded) bytes honestly unavailable without sentinels. A torn final record is ignored.
- Live consistency: RunLog stamps each Write under its existing mutex (so interleaved stdout/stderr offsets match) and appends the timing record before writing the log bytes; readers snapshot the log first, then the sidecar, and ignore coverage beyond the snapshot size. A sidecar write failure stops further timing capture (later lines become unavailable) but never fails the log write. RunLog takes an injectable clock for deterministic tests.
- Truncation: first-MiB records are appended live. Tail records are buffered with the tail, dropped/clipped as tail bytes are dropped, and rebased to final file offsets after the omission marker at Close. The first retained tail fragment gets the receipt time of its first retained byte (its true first byte was omitted).
- Hooks: hookCapture records receipt times for captured output (within its 64 KiB bound); CompleteHook rebases them past the separator/marker and appends them to the sidecar with the hook output.
- Display: CLI uses local time `2006-01-02T15:04:05.000-07:00` (explicit offset, +00:00 for UTC, fixed 29 columns) followed by one space, before any existing match/context line prefix. Unavailable lines show `-` padded to the same width. A continuation of a partial line already prefixed (follow or cursor resume) gets no new prefix. TUI uses the same rules.
- Structured timing is opt-in like CLI display, so existing payloads and agent context cost are unchanged by default: CLI --json with --timestamps, MCP get_run `timestamps: true`, and HTTP API `timestamps=true` add `line_times`: one entry per returned line, including context lines and a cut oversized line, `{"offset": <absolute file offset of the first byte of the line>, "time": "2006-01-02T15:04:05.000Z" | null}`, using the shared UTC run-time format. Existing `log`, `next`, `size`, and cursor fields are unchanged, and `log` is never prefixed. --path with --timestamps is a usage error; --follow rejects --json as before. Resume guidance retains --timestamps.

Non-goals: changing scheduler event logging; rewriting child output; parsing application-provided timestamps; separating stdout/stderr; adding --since, global timestamp preferences, or a new logging dependency; storing timing in SQLite; making tail bytes visible before Close. Do not backfill historical timestamps.

Modified-file contract: internal/store/{log.go,store.go,store_test.go} and a timing file beside them (e.g. internal/store/timing.go); internal/runner/runner.go and runner_test.go only if capture integration requires them; internal/app/{logs.go,history.go,history_test.go,hooks.go}; internal/cli/{root.go,history.go,history_test.go,run.go,run_test.go}; internal/scheduler/serve.go and its existing tests; internal/mcp/{server.go,server_test.go}; internal/httpapi/{api.go,api_test.go}; internal/tui/{tui.go,logview.go,view.go,tui_test.go}; existing shell completion files/tests if needed for the new flag; README.md, docs/{design.md,cli.md,agents.md,hooks.md}, internal/skill/SKILL.md. Justify expansions in task notes before implementation.

Dependencies: NOON-25 (bounded raw log reads), NOON-28 (hook sections), NOON-13 (TUI), all completed. One end-to-end item keeps capture semantics and readers consistent.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 mise exec -- task cli:test PKG="./internal/store ./internal/runner ./internal/app" -- -count=1 -> exit 0. Extend existing critical-path tests after implementation to verify, with an injected clock: first-byte receipt timing across split writes/partial lines and interleaved stdout/stderr, UTC persistence, head/tail truncation rebasing with the omission marker uncovered, hook output timing rebased past its marker, sidecar removal with retention, and that a missing, torn, or short sidecar yields unavailable lines (never neighbouring times) while raw bytes and cursors stay unchanged.
- [x] #2 mise exec -- task cli:test PKG=./internal/cli -- -count=1 -> exit 0. Observable CLI checks cover --timestamps/-t, local offset and millisecond formatting, the unavailable placeholder, prefix order with --match/--context, unchanged default and foreground output, timestamped follow without newline buffering or duplicate prefixes on resumed partial lines, raw cursor continuity across bounded/search/section reads, --json line_times only with --timestamps, and --path with --timestamps rejected.
- [x] #3 mise exec -- task cli:test PKG="./internal/mcp ./internal/httpapi ./internal/tui" -- -count=1 -> exit 0. MCP timestamps:true and HTTP timestamps=true return line_times keyed by absolute line-start offsets; default payloads are byte-for-byte unchanged. TUI shows timestamps by default and toggles them without losing follow/search position; unavailable historical timing renders as the placeholder.
- [x] #4 mise exec -- task cli:build && mise exec -- task ci -> exit 0. Help and README/docs explain opt-in CLI/JSON/MCP/HTTP timestamps, default-on TUI display and toggle, receipt-versus-production time, local display/UTC storage, partial lines, the line_times schema, the sidecar file in the state layout, and historical-log behavior. Manually exercise the built CLI and TUI using isolated HOME/config/state and a local shell job with split writes (e.g. printf partial; sleep 1; printf rest); record exact commands and observed output in task notes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented receipt-time sidecars and timestamp views; raw logs, foreground output, default payloads, and raw-byte cursors remain unchanged. All changed files are within the contract; no tests deleted, skipped, or weakened. Docs/help describe implemented behavior.

AC#1: mise exec -- task cli:test PKG="./internal/store ./internal/runner ./internal/app" -- -count=1 -> exit 0. Injected-clock coverage includes split writes, truncated head/tail rebasing, hook timing, missing/torn/failed sidecars, retention.
AC#2: mise exec -- task cli:test PKG=./internal/cli -- -count=1 -> exit 0. After review correction, full CLI package tests passed again; timestamped bounded pages now emit resume guidance with raw cursor and --timestamps.
AC#3: mise exec -- task cli:test PKG="./internal/mcp ./internal/httpapi ./internal/tui" -- -count=1 -> exit 0. After review correction, full TUI tests passed again; timestamp toggle preserves source anchor, selected match, and tail follow across wrapping changes. gopls diagnostics clean.
AC#4: mise exec -- task cli:build && mise exec -- task ci -> exit 0 in worktree and again on integrated implementation commit 34a3bb5. Security, formatting, vet, Staticcheck, all tests, race tests, and smoke passed.

Manual checks used /tmp/noon61-manual/project/noon.yaml with shell command: printf partial; sleep 1; printf " rest\\nsecond\\n".
From /tmp/noon61-manual/project, run each command with:
env -i PATH=/usr/bin:/bin:/opt/homebrew/bin HOME=/tmp/noon61-manual/home XDG_CONFIG_HOME=/tmp/noon61-manual/config XDG_STATE_HOME=/tmp/noon61-manual/state NOON_STATE_DIR=/tmp/noon61-manual/state/noon /Users/brett/dev/me/noon/.worktrees/feat-log-timestamps/bin/noon
Arguments: run split -> raw stdout "partial rest" then "second".
Arguments: logs split --timestamps -> "2026-10-19T13:24:13.344-06:00 partial rest" and "2026-10-19T13:24:14.352-06:00 second".
TUI command (same project cwd):
expect -c 'set timeout 8; log_file -noappend /tmp/noon61-manual/tui.typescript; spawn -noecho env -i PATH=/usr/bin:/bin:/opt/homebrew/bin HOME=/tmp/noon61-manual/home XDG_CONFIG_HOME=/tmp/noon61-manual/config XDG_STATE_HOME=/tmp/noon61-manual/state NOON_STATE_DIR=/tmp/noon61-manual/state/noon /Users/brett/dev/me/noon/.worktrees/feat-log-timestamps/bin/noon tui; sleep 1; send "l"; sleep 1; send "t"; sleep 1; send "t"; sleep 1; send "?"; sleep 1; send "q"; expect eof'
Observed timestamps by default, "Log timestamps off." after t, and help listing the toggle. Fixture/transcript retained outside checkout; smoke fixtures retained by smoke script.

Single independent review found two P2 issues (missing CLI resume guidance and TUI wrapped-row viewport jump); both fixed with focused regression coverage and package checks. No further validated metadata defects. Initial executor timed out during validation; resumed its preserved work and completed checks.

Delivery: implementation 358c573, merged locally into main as 34a3bb5; no push. Worklease claim e87df1c6a86d9079cb8978c5861115bd, authority 5c3be162a660fb7434fde16d059bdd47, worker 01a1554e-4dac-7248-b935-c64beb629e2a. Created worktree /Users/brett/dev/me/noon/.worktrees/feat-log-timestamps, branch feat/log-timestamps, creation commit 2f5464bcd27d69734056dddcb808d0a801bf5a9b; receipt verified in .git/worktrees/feat-log-timestamps/agent-creation.json against this session before removal. All delegated runs finished. Worktrunk removed integrated clean worktree and branch; exact associated Herdr workspace wHJ had only idle zsh and is verified gone. No retained task worktrees or blockers.

Post-completion review 2026-10-19 of 358c573: implementation matches the settled contract. Verified head records appended before log bytes under the RunLog mutex; tail records clipped with dropped bytes and rebased past the omission marker at Close, with the first retained fragment treated as a line start; hook timing rebased past prefix/marker; removeLog deletes the sidecar; readers snapshot log then sidecar and only query in-snapshot line starts; resumed mid-line slices look up the true line start (no new timestamp), CLI continuation suppresses the duplicate prefix; default CLI/JSON/MCP/HTTP payloads unchanged (line_times only when opted in; --path with --timestamps rejected); TUI default-on with t toggle and anchor preservation. Re-ran AC#1-#3 packages (store, runner, app, cli, mcp, httpapi, tui) with -count=1: all pass except one TestRunner/leader_exits_with_open_grandchild_pipe failure under parallel load. Runner was not changed, and the package passed alone 3 times with -count=3, so this is pre-existing flakiness. Three P3 findings, no P1/P2: (1) internal/store/timing.go newTimingFile opens with O_EXCL and silently skips capture if a sidecar already exists. The log itself is created O_EXCL, so any existing RUN_ID.log.timing is stale: an orphan left by a partial removeLog failure, a manual log deletion, or a restored DB reusing a run ID. The reader then attributes stale times to the new log, which breaks the never-wrong-timing rule. Fix: replace or truncate the stale sidecar (O_TRUNC) after the exclusive log create succeeds. (2) internal/store/log.go disableTiming removes the whole sidecar on any write/sync failure. That makes already-captured lines unavailable too, while the settled design says only later lines become unavailable. The result is conservative but deviates from that wording. (3) RunLog.tailTiming keeps one 24-byte record per post-1MiB write, and clipTailTiming shifts the slice on every drop. With sub-24-byte writes this grows to ~24 MiB per run, and the per-write memmove exceeds the existing 1 MiB tail copy. Pipe-read coalescing limits this in practice. No source fixes made in this review.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented optional timestamped CLI/JSON/MCP/HTTP logs and default-on TUI timestamps, preserving raw bytes and cursors. Reviewed, verified, committed and merged locally; owned worktree and workspace cleaned up. No push.
<!-- SECTION:FINAL_SUMMARY:END -->
