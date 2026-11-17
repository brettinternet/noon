---
id: NOON-89
title: Log persistent config errors once instead of every second
status: done
type: bug
priority: high
labels: [scheduler, delight]
related: [NOON-91]
refs: [docs/design.md, docs/service.md]
files: [internal/scheduler/serve.go, internal/scheduler/serve_test.go]
created: 2026-11-17T18:40:16Z
updated: 2026-11-17T18:51:57Z
---

Why: when a served project's config fails to load (a typo such as `runn:`, a moved or deleted checkout, an unreadable file), the scheduler writes the same `config error project=... error=...` line every second until the config changes. Repro: a `.git/noon.yaml` with an unknown job key under `noon serve` for 6s logs 7 identical lines. `scheduler.log` is not rotated (docs/service.md), so a forgotten typo grows it without bound and buries every run event in the one place users look to learn what happened. Cause: `reloadProject` sets `cached.configSelection` only after a successful load, so the serve loop sees `selectionChanged` on every tick and forces a reload and a log line.

Outcome: one log line per distinct error; nothing more until the config changes.

```console
$ noon serve
2026-11-17 09:00:01 config error project="/src/app" error="..."
# later, after the user edits .git/noon.yaml into a different mistake:
2026-11-17 09:14:22 config error project="/src/app" error="..."
```

Settled contract:
- A project whose config failed to load is retried only when its config stamps or its config selection change, exactly like a loaded project. Track the attempted selection in its own field. `cached.configSelection` keeps naming the selection of the last valid config, because `loadedConfig` relies on it to keep running that config.
- `config error` is logged when the project's recorded error message differs from the last one logged for it: first failure, a different error, or an error after recovery. The same rule covers the temporary-job and webhook-secret errors logged after a successful load.
- A scheduler restart logs each current error once. The recorded project config error in the store (`SetProjectConfigError`), the dashboard notice, and the last-valid-config behavior are unchanged.
- A missing checkout is retried when its paths reappear (stamps change), as today.

Scope: the serve loop's reload trigger and config-error logging in internal/scheduler/serve.go, and a regression test.

Tests (critical only):
- Regression of the subtle bug: with injected time, a broken config across many ticks logs one `config error`; changing it to a different error logs one more; a project that never loaded (fresh start on a broken config) does not reload every tick. Extend TestServeConfigSelectionRecovery or TestServeUserProjectIsolationAndLastGoodReload.

Non-goals: log rotation; rate limiting other scheduler events; a recovery event; notifications; changes to the dashboard or `noon doctor`.

Dependencies: none. Related: NOON-91 makes the moved-checkout case disableable.

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [x] After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `noon_ac_home=$(mktemp -d)` and a temp Git repo whose noon.yaml defines `tests` (schedule `every 1h`, run ["true"]), after `bin/noon enable` and writing `.git/noon.yaml` with `jobs: {typo: {schedule: every 1h, runn: ["true"]}}`: `bin/noon serve 2>"$noon_ac_home/serve.log" & pid=$!; sleep 6; kill $pid` leaves exactly one line containing `config error` in serve.log (today: 7).
  Evidence: task cli:build; serve 6s on typo config: 1 config error line (was 7)
- [x] In the same environment, after removing `.git/noon.yaml`, a second 6s `bin/noon serve` run logs no `config error` line.
  Evidence: config removed; serve 6s: 0 config error lines
- [x] `task cli:test PKG=./internal/scheduler -- -count=1` exits 0 and covers, with injected time: a broken config across at least 5 ticks logs `config error` once; editing it to a different error logs it once more; a project whose first load fails is not reloaded on every tick; the last valid config keeps dispatching while the file is broken.
  Evidence: task cli:test PKG=./internal/scheduler -- -count=1: ok; TestServeLogsPersistentConfigErrorOnce covers all four behaviors

## Definition of done

- [x] task ci passes on the final commit
  Evidence: task ci on main at chore: pin staticcheck to 0.8.2 (after 224e89a): exit 0 (security, check incl. staticcheck, test, race, smoke)
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
  Evidence: Implementation Notes contain AC#1-AC#3 evidence lines
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
  Evidence: git show --stat 224e89a: only internal/scheduler/serve.go and serve_test.go
- [x] No test was deleted, skipped, or weakened
  Evidence: Only additions in serve_test.go; no test removed or skipped
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
  Evidence: No docs/help change needed: docs only say scheduler.log is not rotated; no contract describes per-tick logging

## Notes

Implementation Notes: attemptedSelection tracks the last load attempt so a never-loaded project reloads only on stamp/selection change; configSelection still names the last valid config. loggedError dedupes config error lines (load, temporary-job, webhook-secret) per distinct message; recovery resets it. Test: new TestServeLogsPersistentConfigErrorOnce in serve_test.go (allowed file) instead of extending the two suggested tests, to keep phases isolated; it fails on main without the fix. Commit 224e89a, fast-forwarded to main.
AC#1: task cli:build + /tmp scenario (isolated HOME/XDG/NOON_STATE_DIR, typo runn:) -> serve 6s: 1 config error line.
AC#2: same env, .git/noon.yaml removed, serve 6s -> 0 config error lines.
AC#3: task cli:test PKG=./internal/scheduler -- -count=1 -> ok 3.9s; covers 6+6 ticks across two distinct errors logging 2 lines total, last-valid run at +1m succeeded, restart on broken config logs once and is not reloaded on unchanged ticks (store-error probe stays clear).
DoD task ci: security, gofmt, vet, git-cliff, test, race, smoke all pass. staticcheck via mise exec staticcheck@0.8.2 passes; the pinned tool step fails on main independently: mise.toml staticcheck = "latest" (commit 00f52c9) resolves to 2026.2.1, which the go backend cannot provide.

## Summary

Serve loop tracks attemptedSelection separately from the last valid configSelection, so a failing config is retried only on stamp or selection change, and logs config error once per distinct message (load, temporary-job, webhook-secret). Commit 224e89a with regression test TestServeLogsPersistentConfigErrorOnce; staticcheck pinned to 0.8.2 so task ci passes. Merged to main.
