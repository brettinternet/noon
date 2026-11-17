---
id: NOON-93
title: Add noon projects to show every project the scheduler serves
status: done
type: feature
priority: medium
labels: [cli, delight]
blocked_by: [NOON-91, NOON-92]
refs: [docs/design.md, docs/cli.md]
files:
  - internal/app/projects.go
  - internal/cli/root.go
  - internal/cli/dashboard.go
  - internal/cli/dashboard_test.go
  - README.md
  - docs/cli.md
  - docs/design.md
  - internal/skill/SKILL.md
created: 2026-11-17T18:40:24Z
updated: 2026-11-17T19:10:29Z
---

Why: one per-user scheduler serves every enabled project, but the CLI only ever shows one project at a time. "What is noon running on this machine, and is any of it broken?" has no answer short of remembering every repo or reading `scheduler.log`. MCP `list_projects` and HTTP `GET /v1/projects` already list enabled projects for agents and integrations; people at a terminal have nothing.

Outcome:

```console
$ noon projects
PROJECT  PATH          STATE             NEXT
app      ~/src/app     tests failing     Tue 02:00
site     ~/src/site    ok                Tue 09:00
old      ~/src/old     missing checkout  -
Suggested action: noon -C ~/src/old disable
```

Settled contract:
- `noon projects` is read-only, takes no arguments, ignores `-C`, and creates no state; with no state database or no enabled projects it prints `No enabled projects.` and exits 0.
- One row per enabled project from `app.ListProjects`, sorted by path. PROJECT is the configured name, PATH abbreviates `$HOME` to `~`, NEXT is the earliest `next_at` across the project's jobs using the list rules, STATE is the first that applies: `missing checkout`, `config error`, `JOB failing` / `N jobs failing`, `ok`.
- Per-project data reuses `app.Dashboard` (as MCP and HTTP do), so state matches what bare `noon -C PATH` shows; a missing checkout is reported without loading config.
- One `Suggested action:`, by priority: missing checkout → `noon -C PATH disable`; config error → `noon -C PATH config check`; scheduler offline → `noon service install` (`noon serve` if installed); failing → `noon -C PATH logs JOB -s failed`; otherwise none.
- `--json` prints `{schema_version: 1, projects: [{id, name, path, state, config_error?, failing?: [JOB], next_at?}], next?}` with `state` one of `ok`, `missing`, `config_error`, `failing`. Exit 0 whenever the state could be read.
- README command table, docs/cli.md, and internal/skill/SKILL.md (if it lists commands) describe it.

Scope: new CLI command and app view over existing project and dashboard readers; docs.

Tests (critical only):
- One CLI test with two enabled projects, one healthy and one whose checkout was removed, asserting rows, state, the suggested action, and that the command creates no state database when none exists.

Non-goals: changing MCP `list_projects` or HTTP project shapes; disabled projects; per-job rows; acting on projects (enable, disable, pause) from this command; `--user` special handling beyond what `ListProjects` returns.

Dependencies: NOON-91 (the missing-checkout suggested action must work) and NOON-92 (failing state).

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [x] After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `noon_ac_home=$(mktemp -d)`: `bin/noon projects` exits 0, prints `No enabled projects.`, and leaves no state database under NOON_STATE_DIR.
  Evidence: AC#1: ac.sh -> exit 0, 'No enabled projects.', no NOON_STATE_DIR created.
- [x] In the same environment with temp Git repos `app` (noon.yaml: `flaky` schedule `every 1h` run ["false"]) and `site` (noon.yaml: `tests` schedule `every 1h` run ["true"]), after `bin/noon -C app enable`, `bin/noon -C site enable`, and one `bin/noon -C app run flaky`: `bin/noon projects` exits 0 with one row per project, `app` in state `flaky failing` and `site` `ok`, each with a NEXT time, and `Suggested action: noon -C <app path> logs flaky -s failed`.
  Evidence: AC#2: ac2.sh with serve running -> app 'flaky failing', site ok, both NEXT set, 'Suggested action: noon -C ~/app logs flaky -s failed' (scheduler-online per recorded decision).
- [x] In the same environment, after `mv site site-moved`: `bin/noon projects --json` exits 0 with `schema_version` 1, `site` in state `missing`, and `next` equal to `noon -C <site path> disable`; running that command and then `bin/noon projects --json` lists only `app`.
  Evidence: AC#3: ac.sh -> schema_version 1, site missing, next 'noon -C <site path> disable'; ran it exit 0; then only app listed.
- [x] `task cli:test PKG=./internal/cli -- -run TestProjects -count=1` exits 0 and covers a healthy and a missing-checkout project, the suggested action, and no state creation without a database.
  Evidence: AC#4: task cli:test PKG=./internal/cli -- -run TestProjects -count=1 -> ok.

## Definition of done

- [x] task ci passes on the final commit
  Evidence: task ci exit 0 on the merged tree (1b489be).
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
  Evidence: Notes have AC#1-4 lines with commands and results.
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
  Evidence: Diff limited to contract files: app/projects.go, cli/root.go, cli/dashboard.go, cli/dashboard_test.go, README.md, docs/cli.md, docs/design.md.
- [x] No test was deleted, skipped, or weakened
  Evidence: Only TestProjects added; no tests changed.
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
  Evidence: README table, docs/cli.md Projects section, design.md Experience bullet describe implemented behavior.

## Notes

AC2 conflict: contract ranks scheduler offline above failing, but AC2 had no scheduler and expected the logs suggestion. User decided (2026-11-17): keep contract order; AC2 is verified while bin/noon serve is running so the scheduler is online.

AC#1: /tmp/noon93-ac/ac.sh (task cli:build; HOME/XDG_CONFIG_HOME/XDG_STATE_HOME/NOON_STATE_DIR under one mktemp -d) -> bin/noon projects exit 0, 'No enabled projects.', NOON_STATE_DIR absent afterwards.
AC#2: /tmp/noon93-ac/ac2.sh (same env; app flaky run false, site tests run true; both enabled; bin/noon -C app run flaky; bin/noon serve running per the recorded AC2 decision) -> exit 0; rows app ~/app 'flaky failing' NEXT Tue 13:09, site ~/site ok NEXT Tue 13:09; 'Suggested action: noon -C ~/app logs flaky -s failed'. Without a scheduler the contract's offline priority suggests noon service install (observed).
AC#3: ac.sh after mv site site-moved -> projects --json exit 0, schema_version 1, site state missing, next 'noon -C /private/var/.../site disable'; running it with bin/noon exit 0; projects --json then lists only app.
AC#4: task cli:test PKG=./internal/cli -- -run TestProjects -count=1 -> ok; covers healthy + missing-checkout project rows, next/suggested action (JSON and human), and no state dir created without a database.
CI: task ci exit 0 on c7b1fcd+change (tree identical to merged 1b489be; later main commit is backlog-only). Commit 1b489be fast-forwarded onto main; worktree and branch removed.
Review: one inline pass; fixed suggested --state-dir being appended when it only came from NOON_STATE_DIR (env already carries over). SKILL.md has no command list, so unchanged.
