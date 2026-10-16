---
id: NOON-44
title: Keep human job listings unambiguous and offer full-width inspection
status: To Do
assignee: []
created_date: '2026-10-16 23:22'
updated_date: '2026-10-16 23:23'
labels:
  - cli-ux
  - devex
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
- [ ] #1 `task cli:build` exits 0. With long-job-name-alpha and long-job-name-beta in an isolated fixture, list/history/dashboard distinguish both targets at normal terminal widths; the documented full-width list invocation exposes complete names and commands, including when redirected. Record real-terminal checks at 80 and 120 columns and a redirected-output check.
- [ ] #2 `task cli:test -- -run '^(TestListJSON|TestDashboard)$' -count=1 -v` and `task test` exit 0. Preserve the JSON schema and existing escaped-cell safety; add only a focused regression for colliding truncated identifiers or control-character leakage, not help/table snapshots.
- [ ] #3 `task smoke` exits 0; human inspection does not create state or run jobs, no environment values appear, and docs explain compact versus full-width behavior without requiring jq.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
