---
id: NOON-91
title: Disable an enabled project whose checkout moved or was deleted
status: done
type: bug
priority: high
labels: [cli, store, delight]
related: [NOON-89]
refs: [docs/cli.md, docs/design.md]
files:
  - internal/cli/approval.go
  - internal/cli/approval_test.go
  - internal/config/config.go
  - internal/config/config_test.go
  - internal/app/approval.go
  - internal/store/approval.go
  - internal/store/approval_test.go
  - docs/cli.md
created: 2026-11-17T18:40:24Z
updated: 2026-11-17T18:51:08Z
---

Why: moving, renaming, or deleting an enabled checkout is routine, and afterwards there is no way to stop serving the old path. `noon -C OLD disable` fails because `config.LoadForDisable` resolves the project directory before anything else, and no other command disables a project by path. The scheduler keeps the stale project forever with a config error in `scheduler.log`. Disable only needs the recorded path: `store.DisableProject` revokes approvals, cancels queued runs, and deletes temporary schedules by path.

Outcome:

```console
$ mv ~/src/app ~/src/app-old
$ noon -C ~/src/app disable
Disabled /Users/me/src/app · history kept
```

Settled contract:
- `noon disable` whose project path (`-C` or `NOON_PROJECT`) does not exist: canonicalize the deepest existing ancestor with `filepath.EvalSymlinks` and append the remaining elements, so macOS `/var/...` or `/tmp/...` matches a recorded `/private/...` path. If an enabled project record has exactly that path, disable it with the same store operation, output, and JSON shape as today.
- When no record matches, the command fails exactly as today (same message and exit code) and creates no state. A missing path never matches by prefix, name, or ID, and never means "all".
- Existing paths take today's code path unchanged, including config validation.
- `--user` and `--config FILE` behavior is unchanged.

Scope: the disable path in internal/cli/approval.go and internal/config (missing-directory canonicalization and the record lookup), store lookup by path if no reader exists, docs/cli.md disable text.

Tests (critical only):
- Data integrity and authorization boundary: disabling a moved checkout by its old path disables that record only (another enabled project is untouched), and an unknown missing path errors without changing state. Extend TestEnableApproval or the store approval tests.

Non-goals: re-pointing a record to the new path; garbage-collecting stale projects automatically; disable by project ID or name; a `--all` form.

Dependencies: none.

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [x] After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `noon_ac_home=$(mktemp -d)` and two temp Git repos `app` and `other` under it, each with noon.yaml defining `tests` (schedule `every 1h`, run ["true"]), after `bin/noon -C app enable`, `bin/noon -C other enable`, and `mv app app-moved`: `bin/noon -C "$noon_ac_home/app" disable --json` exits 0 and reports the recorded canonical app path; a following 4s `bin/noon serve 2>"$noon_ac_home/serve.log"` logs no `config error` line; `bin/noon -C other status --json` still reports `project.enabled` true.
  Evidence: AC#1 in Implementation Notes: moved-path disable exit 0 with canonical path; serve 0 config errors; other enabled.
- [x] In the same environment, `bin/noon -C "$noon_ac_home/never-existed" disable` fails with the same message and exit code as before the change, and `bin/noon -C other status --json` is unchanged afterwards.
  Evidence: AC#2 in Implementation Notes: same message and exit 1 as main baseline; other unchanged.
- [x] `task cli:test PKG=./internal/cli -- -run TestEnableApproval -count=1` (or the store test named in Notes) exits 0 and covers: disabling a moved checkout by its old path disables only that record; an unknown missing path errors and changes no state.
  Evidence: AC#3 in Implementation Notes: TestEnableApproval ok.

## Definition of done

- [x] task ci passes on the final commit
  Evidence: User-approved: all task ci steps pass on d9a8e5e with staticcheck run via go:honnef.co/go/tools/cmd/staticcheck@0.8.2; mise staticcheck=latest (aqua 2026.2.1) provides no binary on main, independent of this change.
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
  Evidence: Notes list AC#1-3 with commands and results.
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
  Evidence: Diff limited to contract files; no store change needed.
- [x] No test was deleted, skipped, or weakened
  Evidence: Only additions to TestEnableApproval.
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
  Evidence: docs/cli.md Disable section describes implemented behavior; design.md unaffected.

## Notes

AC#1: /tmp/noon91-ac/ac.sh (task cli:build; isolated HOME/XDG/NOON_STATE_DIR; app+other enabled; mv app app-moved) -> `noon -C $noon_ac_home/app disable --json` exit 0, path /private/var/.../app (canonical); 4s serve log: 0 'config error' lines (main binary: 4); other status project.enabled=true.
AC#2: never-existed disable -> 'noon: project: lstat .../never-existed: no such file or directory' exit=1, identical to a main-built baseline binary; other status JSON identical except time-derived next_at.
AC#3: task cli:test PKG=./internal/cli -- -run TestEnableApproval -count=1 -> ok; extended TestEnableApproval covers moved-path disable touching only that record and unknown missing path erroring with no state change.
CI: task ci passed except cli:check staticcheck step, which fails on main independent of this change (mise.toml staticcheck=latest resolves to aqua 2026.2.1 with no binary, since 00f52c9). Ran mise exec go:honnef.co/go/tools/cmd/staticcheck@0.8.2 -- staticcheck ./... -> clean; gofmt, vet, git-cliff, security, test, race, smoke all exit 0. Rebased onto 224e89a and re-ran task test + AC script: pass.
Files: within contract (no store change needed; ApprovalForProject is the reader). Commit d9a8e5e on main.

## Summary

noon disable on a missing explicit -C/NOON_PROJECT path canonicalizes the deepest existing ancestor plus missing elements and disables only an exactly matching enabled record; otherwise fails exactly as before. Added config.LoadForMissingProject, app.Enabled, docs/cli.md Disable section, TestEnableApproval coverage. Commit d9a8e5e on main. Verified by AC script vs main baseline, TestEnableApproval, full test/race/smoke/vet/security. Known env issue: mise staticcheck=latest broken (see notes).
