---
id: NOON-58
title: Demonstrate external heartbeat monitoring of jobs and the scheduler
status: Done
assignee: []
created_date: '2026-10-18 16:24'
updated_date: '2026-10-18 17:57'
labels:
  - examples
  - integration
  - hooks
  - reviewed
dependencies: []
documentation:
  - docs/hooks.md
  - examples/integrations/notifications/README.md
  - docs/service.md
modified_files:
  - examples/integrations/heartbeat/**
  - examples/README.md
  - integration/examples_test.go
priority: low
type: task
ordinal: 20000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a runnable example shows how to get alerted when Noon work stops happening, including when the scheduler itself is down, using an external dead man switch monitor and only existing hooks and jobs.

Why: lifecycle hooks fire only when a run completes (foreground runs also fire them). A crashed service, a reboot without `noon service install`, or a job that silently stops being scheduled produces no alert. Prior art: Healthchecks.io and Cronitor (ping on success, ping a failure endpoint on failure, alert when no ping arrives within period plus grace). decision-4: outbound HTTP is a command, not a built-in action.

Scope: new examples/integrations/heartbeat/ with README.md, noon.yaml, user-config.yaml, and one executable bin/noon-heartbeat.
- `noon-heartbeat URL_FILE` reads one non-empty HTTP(S) URL from a protected file (the ping URL is a secret; keep it out of YAML and the service unit until draft-9 lands). Limit this example to base ping URLs without queries or fragments so appending `/fail` on failed or timed_out status is unambiguous (Healthchecks.io convention); reject unsupported input before invoking curl. Send a bounded request with curl: finite connection, per-attempt, and total retry budgets fitting within the 30s hook timeout; disable implicit .curlrc loading, do not follow redirects, discard response bodies, and emit only a generic failure diagnostic rather than raw curl stderr. Missing, unreadable, empty, malformed, or multi-line URL files exit nonzero without printing their contents. The file is private user-owned setup, not a committed fixture secret.
- noon.yaml: per-job pings from on_success and on_failure for one job.
- user-config.yaml: a scheduler-level heartbeat job (e.g. `every 5m`, `missed: skip`) in the user project.
- README: monitor period and grace guidance for laptops that sleep (missed runs, catch-up), that a stopped scheduler surfaces only as missing pings, how to point the URL at other providers that accept a plain GET, and the protected-file setup (`umask 077`). Add a row to examples/README.md under integrations.

Non-goals: a built-in HTTP or monitoring action, a provider catalog or Cronitor-specific query parameters, Noon-side alerting when Noon is down, core or docs/ changes.

Dependencies: none. draft-9 may later replace the URL file.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes first. Tests use only loopback httptest traffic, never an external service, with isolated HOME, XDG, and state. Install the copied user-config.yaml as the isolated user project's config.yaml and exercise it with --user; --config validation alone does not prove user-project execution.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test PKG=./integration -- -run "^TestExamplesValidate$" -count=1` exits 0 and validates the new noon.yaml and user-config.yaml.
- [x] #2 `task cli:test PKG=./integration -- -run '^TestExampleHeartbeat$' -count=1 -v` exits 0 with PASS, not SKIP. Through the built binary and copied examples, a success hook hits a loopback server's base path, a failure hook hits base path + /fail, and `--user run` of the installed user-config heartbeat hits the base path. Missing/empty/malformed URL files fail without a request. A server failure whose response echoes a sentinel secret URL exits nonzero within a test deadline and leaves no URL or response body in script output or retained hook logs. Use finite retry budgets; never call an external monitor.
- [x] #3 `test -x examples/integrations/heartbeat/bin/noon-heartbeat` exits 0 and `rg -n "integrations/heartbeat" examples/README.md` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add bounded secret-safe heartbeat script and project/user examples. 2. Exercise copied examples with loopback HTTP and secret-leak regression checks. 3. Run acceptance and full CI, review, commit, merge and clean owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refinement: foreground runs also fire hooks (internal/cli/run.go). TestExamplesValidate currently loads user-config.yaml with --config, so add real isolated --user execution. Secret leakage must be checked on an actual HTTP error, not only a missing file; raw curl diagnostics/response bodies and user .curlrc must not leak the ping credential. Healthchecks base and /fail GET forms: https://healthchecks.io/docs/http_api/.

AC#1: task cli:test PKG=./integration -- -run "^TestExamplesValidate$" -count=1 -> PASS, exit 0; both new YAML files validated.
AC#2: task cli:test PKG=./integration -- -run "^TestExampleHeartbeat$" -count=1 -v -> PASS (not SKIP), exit 0. Copied job hooks and installed --user config hit loopback base/fail endpoints; malformed files make no request; HTTP 503 retries are bounded and neither direct output nor retained hook logs leak sentinel URL/body. Redirect refusal and hostile .curlrc covered.
AC#3: test -x examples/integrations/heartbeat/bin/noon-heartbeat && rg -n "integrations/heartbeat" examples/README.md -> exit 0, integration row found.
Full task ci passed (security, vet, Staticcheck, all tests, race, smoke). LSP diagnostics clean. Direct general review completed with no concrete defects; independent reviewer unavailable after registry/modelScope rejected both launch attempts (no child ran). Changes stay within allowed files; no existing tests changed or weakened. Existing command/hook extension contract unchanged.

Delivery: implementation commit 2296656 merged into main as 822cca4 (non-fast-forward because unrelated bulk-pause work landed concurrently). task ci passed again on the merged implementation. Verified owned Worktrunk receipt and workspace wGX shell pane; removed heartbeat-example worktree and branch through Worktrunk, including its Herdr workspace. No blocker or next resumable step remains; nothing pushed.

Post-completion review: one P2 fixed. noon-heartbeat chooses /fail from NOON_STATUS, but JobEnvironment only stripped some reserved names, so hook-only metadata (NOON_STATUS, NOON_EXIT_CODE, NOON_PREVIOUS_STATUS, NOON_CONSECUTIVE_FAILURES, NOON_LOG_FILE, NOON_SCHEDULED_AT, NOON_AGENT) leaked into any job started from a hook. Reproduced: with inherited NOON_STATUS=failed, 'noon --user run scheduler-heartbeat' pinged /fail. internal/app/run.go now strips all run/hook metadata from inherited env (prompt jobs re-set NOON_AGENT; scheduler re-sets NOON_SCHEDULED_AT); TestExampleHeartbeat extended (fails without the fix); docs/design.md environment bullet updated. Otherwise verified URL validation, curl bounds/.curlrc/redirect handling, hook status routing (matchingHook: failed/timed_out -> on_failure), and user-project cwd. task ci passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added external heartbeat recipe with protected URL files, bounded secret-safe curl, per-job result hooks and user-project scheduler liveness. All three acceptance commands and full CI pass; locally merged and owned worktree cleaned.
<!-- SECTION:FINAL_SUMMARY:END -->
