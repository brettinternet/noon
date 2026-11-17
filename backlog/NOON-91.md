---
id: NOON-91
title: Disable an enabled project whose checkout moved or was deleted
status: todo
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
updated: 2026-11-17T18:40:46Z
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

- [ ] After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `noon_ac_home=$(mktemp -d)` and two temp Git repos `app` and `other` under it, each with noon.yaml defining `tests` (schedule `every 1h`, run ["true"]), after `bin/noon -C app enable`, `bin/noon -C other enable`, and `mv app app-moved`: `bin/noon -C "$noon_ac_home/app" disable --json` exits 0 and reports the recorded canonical app path; a following 4s `bin/noon serve 2>"$noon_ac_home/serve.log"` logs no `config error` line; `bin/noon -C other status --json` still reports `project.enabled` true.
- [ ] In the same environment, `bin/noon -C "$noon_ac_home/never-existed" disable` fails with the same message and exit code as before the change, and `bin/noon -C other status --json` is unchanged afterwards.
- [ ] `task cli:test PKG=./internal/cli -- -run TestEnableApproval -count=1` (or the store test named in Notes) exits 0 and covers: disabling a moved checkout by its old path disables only that record; an unknown missing path errors and changes no state.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
