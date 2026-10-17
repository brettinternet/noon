---
id: NOON-44
title: Keep human job listings unambiguous and offer full-width inspection
status: Done
assignee: []
created_date: '2026-10-16 23:22'
updated_date: '2026-10-17 02:29'
labels:
  - cli-ux
  - devex
  - reviewed
dependencies: []
modified_files:
  - internal/cli/jobs.go
  - internal/cli/history.go
  - internal/cli/dashboard.go
  - internal/cli/root.go
  - internal/cli/*_test.go
  - docs/cli.md
priority: medium
type: enhancement
ordinal: 11400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users can identify and copy the right job and inspect its full command without having to decode JSON.

Verified reproduction: jobs long-job-name-alpha and long-job-name-beta both render as long-job-name... in noon list. printJobs caps job names at 16 characters and assumes an 80-column terminal, printHistory caps names at 18, and the dashboard also hard-codes 80. Distinct targets become indistinguishable even on a wide terminal.

Scope: make normal human list/history/dashboard output distinguish jobs with shared prefixes and provide a documented full-width human inspection option on list (--wide) that retains complete names, schedules and command text. Use available terminal width sensibly with a deterministic redirected-output fallback; keep compact defaults readable. Human output must retain control-character escaping and must never reveal environment values. Preserve JSON as the lossless machine interface.

Non-goals: a new TUI, automatic pager, ANSI styling framework, redesigning run/log serialization or exposing raw untrusted terminal controls.
Modified-file contract: internal/cli/jobs.go, history.go, dashboard.go, root.go and existing CLI test files; docs/cli.md. Explain expansion.
Dependencies: none; do not require NOON-38 for a new long-form full-width option.

Acceptance commands describe the intended implementation, not current behavior. Isolate HOME, XDG_CONFIG_HOME, XDG_STATE_HOME and NOON_* settings in temporary fixtures; never touch real jobs or services. Implement before adding tests, extend existing public-interface tests only for parsing, safety, or realistic regressions, and record command/result evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:build` exits 0. With long-job-name-alpha and long-job-name-beta in an isolated fixture, list/history/dashboard distinguish both targets at normal terminal widths; the documented full-width list invocation exposes complete names and commands, including when redirected. Record real-terminal checks at 80 and 120 columns and a redirected-output check.
- [x] #2 `task cli:test -- -run '^(TestListJSON|TestDashboard)$' -count=1 -v` and `task test` exit 0. Preserve the JSON schema and existing escaped-cell safety; add only a focused regression for colliding truncated identifiers or control-character leakage, not help/table snapshots.
- [x] #3 `task smoke` exits 0; human inspection does not create state or run jobs, no environment values appear, and docs explain compact versus full-width behavior without requiring jq.
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
Implementation plan: preserve complete escaped job identifiers in list/history/dashboard; reuse the bounded table renderer for compact output using stdout terminal size (80-column fallback), and disable truncation for list --wide. Contract expansion: go.mod promotes the already-pinned github.com/charmbracelet/x/term dependency from indirect to direct for portable terminal-size detection; no new module or version. No design contract changes.

AC#2: task cli:test -- -run '^(TestListJSON|TestDashboard)$' -count=1 -v and task test -> exit 0; focused TestListHumanIdentifiers covers shared-prefix collisions and wide-mode control escaping. Existing JSON shape and inspection snapshot checks preserved and extended to --wide. AC#3: task smoke -> exit 0; TestListJSON verifies wide inspection does not change files or emit environment values. docs/cli.md documents compact, wide, escaping, and redirected fallback. Source commit e98731c; task check:staged and task ci on that commit -> exit 0 (security, vet/staticcheck, full tests, race, smoke). CGO_ENABLED=0 go build -> exit 0. LSP diagnostics clean for all changed Go files. Terminal-width verification is in progress; integration pending preservation of concurrent NOON-46 edits in main.

AC#1: task cli:build -> exit 0. Independent verifier used macOS script PTYs with stty cols 80 and 120 for list/history/status, plus redirected list --wide: both long-job-name-alpha and long-job-name-beta remain complete; wide schedules/commands are complete. Isolated inspection created no state before two explicit harmless printf runs; environment sentinel absent and control-escaping regression passed. Review: one independent verification/diff pass, no concrete defects. Report: /tmp/noon-44-verification.md. Main NOON-46 work committed independently; merged its changes into the task branch preserving both help updates, task ci -> exit 0 on 3d063bf. Integrated to main as d0e2ff2. No tests deleted/skipped/weakened; only documented go.mod contract expansion. Remaining delivery: owned worktree cleanup and final backlog commit; no blocker.

Review fix: printBoundedRows protected only one identifier column, so history could truncate run IDs of 5+ digits (12345 -> 1...) beside long job names, leaving the ID unusable for noon logs. It now accepts several protected columns; history protects RUN and JOB. Verified with a direct render probe; task cli:test passes.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented complete job identifiers in compact tables and list --wide with escaped full schedules/commands. Terminal width is detected with deterministic 80-column redirected fallback. Source e98731c, integration d0e2ff2; full CI passes on integrated source. Independent PTY and redirected verification passed with no defects. Owned fix/list-wide worktree and branch removed through Worktrunk; its Herdr workspace closed. Existing feat/tui-design worktree is unrelated and retained. No blocker or remaining implementation step; no push performed.
<!-- SECTION:FINAL_SUMMARY:END -->
