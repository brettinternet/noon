---
id: NOON-51
title: Limit jobs to matching operating systems and architectures
status: Done
assignee: []
created_date: '2026-10-17 17:30'
updated_date: '2026-10-17 21:06'
labels:
  - config
  - scheduler
  - reviewed
dependencies: []
documentation:
  - docs/configuration.md
  - docs/design.md
  - >-
    backlog/decisions/decision-2 -
    Repository-defined-jobs-run-unattended-only-after-approval-of-their-current-definition.md
  - >-
    backlog/decisions/decision-5 -
    Job-semantics-follow-cron-and-launchd-expectations.md
modified_files:
  - internal/config/jobs.go
  - internal/config/jobs_test.go
  - internal/config/config.go
  - internal/config/edit.go
  - internal/app/queue.go
  - internal/app/doctor.go
  - internal/app/inspect.go
  - internal/app/dashboard.go
  - internal/cli/run.go
  - internal/cli/run_test.go
  - internal/cli/jobs.go
  - internal/cli/dashboard.go
  - internal/cli/style.go
  - internal/scheduler/serve.go
  - internal/scheduler/serve_test.go
  - internal/httpapi/webhooks.go
  - internal/httpapi/webhooks_test.go
  - internal/tui/tui.go
  - internal/mcp/server.go
  - noon.schema.json
  - examples/jobs.yaml
  - docs/configuration.md
  - docs/design.md
  - docs/cli.md
  - docs/webhooks.md
priority: medium
type: feature
ordinal: 14800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: some repository jobs only make sense on one platform (brew cleanup on macOS, a systemd check on Linux, a native arm64 build). Today they must check inside the script, which fills history with no-op successes, or fail on the wrong machine. Shell `if:` conditions and per-platform files (noon.darwin.yaml) were considered and rejected: `if:` executes code during evaluation, breaking the rule that validation, list, doctor, and bare noon never run anything, and it hides a second action from approval; platform files add a precedence dimension and force whole-job copies because jobs replace by name.

Outcome: a job may declare static platform constraints that Noon checks without running anything:

```yaml
jobs:
  notarize:
    os: [darwin]
    arch: [arm64]
    run: ./scripts/notarize
```

Settled contract:
- `os` and `arch` are optional, non-empty, duplicate-free lists of strings. Values match Go GOOS/GOARCH for supported platforms only: os `darwin`, `linux`; arch `amd64`, `arm64`. Unknown values are config errors with file, line, and YAML path; common aliases get a suggestion and are never accepted (`macos`/`macOS` -> darwin, `x86_64`/`x64` -> amd64, `aarch64` -> arm64). A scalar is an error.
- A job matches when every declared field contains the running binary's value; an absent field matches everything. Both fields are execution fields: they are in the approval digest, so changing them requires re-approval.
- A non-matching job is inert on this machine: never scheduled, caught up, or dispatched (a queued run is cancelled like a disabled job's). Foreground `noon run` and every queueing path (CLI --detach, MCP run_job, triggers, shell --on-enter, webhooks) reject it with code `unsupported_platform`, exit 1 for CLI, 409 for webhooks, recording no run; the hint names the allowed values and the current platform.
- It stays visible: `list`, the dashboard, and the TUI show NEXT as `other platform` (shown before disabled, paused, or needs approval). `list --json` adds `os` and `arch` (omitted when unset) and `platform_supported` (bool). `doctor` skips command and cwd checks for non-matching jobs instead of reporting them broken.
- `noon enable` approves non-matching shared jobs like any other. A private job that replaces a shared one uses its own fields; the local `{enabled: false}` toggle is unchanged.
- Matching reads GOOS/GOARCH in one place and passes it down, so tests can simulate the other platform on any host.

Scope: parsing, validation, digest, matching, schedule and dispatch eligibility, run and queue rejection, webhook rejection, list/dashboard/TUI/JSON display, doctor skip, schema, examples/jobs.yaml (uses every field), docs: configuration.md (job field table, matching rules), design.md (jobs, approval digest field list), cli.md (error code), webhooks.md (409 reasons).

Tests (critical only): value validation and alias rejection (parsing); os/arch change the digest (authorization boundary); a non-matching job is never scheduled, caught up, or dispatched and its queued run is cancelled (state transition); webhook rejection. Extend existing internal/config, internal/scheduler, internal/httpapi, and internal/cli tests.

Non-goals: other conditions (hostname, user, container, env vars, commands on PATH, AC power, network, Git state) — fixed machine preferences belong in .git/noon.yaml, configuration problems in doctor, and changing state in scripts; shell or expression conditionals; per-platform config files; `noon add`/`import` flags for os/arch; values for unsupported platforms; field-level merging of local overrides; a skipped run status.

Dependencies: none.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `mktemp -d` and a temp Git repo whose noon.yaml defines `here` (os and arch set to `go env GOOS`/`go env GOARCH`, run [true]) and `elsewhere` (os set to the other supported OS, schedule every 1h, run [noon-missing-command]): `bin/noon list --json` reports `here` platform_supported true and `elsewhere` false with its `os`; `bin/noon list` shows `elsewhere` NEXT `other platform`; `bin/noon run here` exits 0; `bin/noon run elsewhere` and `bin/noon run elsewhere --detach` exit 1 with code `unsupported_platform` (checked with --json) and `bin/noon history --json` lists only the `here` run; `bin/noon doctor` does not report noon-missing-command.
- [x] #2 In the same environment, `bin/noon config check` exits 1 with file, line, and YAML path for each of: `os: [macos]` (suggests darwin), `arch: [x86_64]` (suggests amd64), `arch: [aarch64]` (suggests arm64), `os: []`, `os: [linux, linux]`, `os: darwin`, `os: [windows]`. After `bin/noon enable`, changing only `elsewhere`'s os list makes `bin/noon list --json` report it `needs_approval`, while editing its description does not.
- [x] #3 `task cli:test PKG=./internal/scheduler -- -count=1` exits 0 and covers, with injected time and platform: a non-matching scheduled job never runs or catches up across a missed window, a queued run of it is cancelled at dispatch, and a matching job in the same project runs normally.
- [x] #4 `task cli:test PKG=./internal/config -- -count=1`, `task cli:test PKG=./internal/httpapi -- -count=1`, and `task cli:test PKG=./internal/cli -- -count=1` exit 0 and cover alias rejection, digest coverage of os and arch, and a signed webhook for a non-matching job returning 409 without queueing a run.
- [x] #5 noon.schema.json accepts the os/arch enums and rejects other values; examples/jobs.yaml uses both fields and `task cli:test PKG=./integration -- -run '^TestExamplesValidate$' -count=1` exits 0. Docs in scope describe only implemented behavior. `task ci` exits 0.
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
Scope expansion approved: internal/httpapi/api.go must map unsupported_platform from shared app.QueueRun to HTTP 409 alongside disabled/paused/needs_approval, rather than misclassifying an expected execution rejection as HTTP 500. This preserves the existing API error mapping pattern for the new platform gate.

Scope expansion: internal/tui/view.go owns Model.nextCell, the existing TUI NEXT-cell rendering and precedence. The required other platform indicator belongs there, ahead of disabled/paused/approval, rather than an unnecessary refactor into the listed internal/tui/tui.go. Executor reported this file had already been edited before requesting expansion; reviewed the location and authorized retaining the item-scoped change. Further scope expansions must be recorded before editing.

Scope expansion approved before editing: internal/cli/root_test.go enumerates the exact list --json job keys. Add the required platform_supported key to that existing expectation while preserving its exact-key assertion; this aligns the regression test with the new public JSON contract without weakening it.

Corrected AC#3/#4 command syntax to pass -count=1 after Task's -- separator, per docs/development.md. Original commands fail Task option parsing; test scope/outcomes unchanged. User explicitly authorized claim recovery after expiry; both children stopped and no competing claim existed. Reacquired f71117a0137ec28a41dc2d231606a6c6. Independent review: no validated findings. Commit 65f3b4f passed task check:staged and task ci.

AC#1: Executor ran task cli:build and isolated bin/noon list --json, list, run here, run elsewhere --json, run elsewhere --detach --json, history --json, doctor -> matching job ran; unsupported commands exited 1 with unsupported_platform; only here recorded; doctor omitted missing command.
AC#2: Isolated bin/noon config check for all seven listed invalid definitions -> exit 1 with file/line/path and alias suggestions; bin/noon enable and list --json -> description remained approved, OS edit required approval.
AC#3: task cli:test PKG=./internal/scheduler -- -count=1 -> passed; injected platform/time covers missed-window exclusion, queued cancellation and matching catch-up.
AC#4: task cli:test PKG=./internal/config -- -count=1; task cli:test PKG=./internal/httpapi -- -count=1; task cli:test PKG=./internal/cli -- -count=1 -> all passed; parsing, digest, signed webhook 409/no run covered.
AC#5: mise exec -- go run /tmp/noon-51-schema-check.go -> valid enums accepted, invalid values rejected; task cli:test PKG=./integration -- -run '^TestExamplesValidate$' -count=1 -> passed; task ci -> passed both executor and parent on implementation commit 65f3b4f.
Delivery: fast-forward merged 65f3b4f into main. No tests deleted, skipped or weakened. Docs describe implemented behavior. Removed verified session-owned worktree and branch with Worktrunk; confirmed associated Herdr workspace wG2 disappeared. No retained worktree, remaining blocker or next implementation step. No push performed.

Post-completion review 2026-10-17: independent static review found no validated defects. Reviewed complete implementation diff and current validation, alias rejection, approval digest coverage, foreground and queue gates, scheduler cancellation, webhook authentication ordering, and inspection/doctor behavior. Verification: task cli:test PKG='./internal/config ./internal/store ./internal/scheduler ./internal/httpapi ./internal/cli ./internal/mcp ./integration' -- -count=1 -> all seven packages passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented static os/arch constraints with approval digest coverage, execution gates, scheduler cancellation, inspection visibility, schema and docs. Merged 65f3b4f locally; focused checks and CI passed; independent review found no validated defects; worktree/branch/workspace cleaned.
<!-- SECTION:FINAL_SUMMARY:END -->
