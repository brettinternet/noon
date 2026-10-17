---
id: NOON-50
title: Run personal jobs from user config with --user
status: Done
assignee: []
created_date: '2026-10-16 01:43'
updated_date: '2026-10-17 19:15'
labels:
  - config
  - cli
  - scheduler
dependencies: []
documentation:
  - docs/configuration.md
  - docs/design.md
  - >-
    backlog/decisions/decision-2 -
    Repository-defined-jobs-run-unattended-only-after-approval-of-their-current-definition.md
  - >-
    backlog/decisions/decision-3 -
    Runtime-state-lives-per-user-in-SQLite-not-in-the-repository.md
modified_files:
  - internal/config/config.go
  - internal/config/config_test.go
  - internal/config/edit.go
  - internal/config/edit_test.go
  - internal/config/jobs.go
  - internal/config/jobs_test.go
  - internal/cli/root.go
  - internal/cli/root_test.go
  - internal/cli/manage.go
  - internal/cli/manage_test.go
  - internal/cli/import.go
  - internal/cli/import_test.go
  - internal/cli/guidance.go
  - internal/cli/shell.go
  - internal/cli/shell_test.go
  - internal/cli/dashboard.go
  - internal/cli/dashboard_test.go
  - internal/app/dashboard.go
  - internal/app/inspect.go
  - internal/app/manage.go
  - internal/scheduler/serve.go
  - internal/scheduler/serve_test.go
  - internal/mcp/server_test.go
  - integration/user_jobs_test.go
  - examples/local-dev/housekeeping/**
  - examples/README.md
  - README.md
  - docs/configuration.md
  - docs/cli.md
  - docs/crontab.md
  - docs/design.md
priority: medium
type: feature
ordinal: 14700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Why: some recurring work belongs to no repository: a morning digest across repos, machine chores, an imported personal crontab. Today it needs a stand-in project such as examples/local-dev/housekeeping, a Git directory created only to hold `.git/noon.yaml`, or a `noon.yaml` that needs approval after every edit. `noon import crontab --write` can only write a personal crontab into some repository.

Outcome: personal jobs live in user config and are managed with one global flag: `noon --user add|remove|list|run|history|logs|cancel|pause|resume|enable|disable|status|doctor|tui|mcp` and `noon --user import crontab FILE --write`. `noon serve` runs them like any enabled project.

Settled contract:
- The user project is the user config directory (`$XDG_CONFIG_HOME/noon`, else `~/.config/noon`). Its jobs come only from `jobs:` in that directory's `config.yaml`; it never reads `noon.yaml`, `.git/noon.yaml`, or anything above it. Its identity is the canonical directory path, and config.Load applies one rule to it however it is selected, so `--user`, `-C` with that directory, MCP `project` set to that directory, and the selection recorded by `noon enable` all load the same project. Discovery from the working directory is unchanged; use `--user`.
- User jobs are the user's own (decision-2): `shared: false`, approval `not_required`. The project root, the default `cwd`, and the base for relative `cwd`, `prompt_file`, and argv paths is the user config directory, so scripts can sit beside config.yaml. YAML still never expands `~`.
- `jobs` becomes valid in user config. Regular projects never include user jobs, but every load still validates the whole user config, so a broken user job is a config error everywhere, like any user-config error today.
- `--user` conflicts with `-C/--project`, `NOON_PROJECT`, and `--config` (usage error, exit 2). `serve`, `service`, `init`, and `shell` reject it. `add` writes user config, creating the directory (0700) and file (0600) if absent, and rejects `--shared`/`--local`; `remove` and `import crontab --write` edit the same file in place.
- Enablement follows decision-2: `noon --user enable` opts in, and `noon --user add` with a schedule or queued run enables it, as `add` does today. It is an ordinary project in the store, with its own one-active-run slot under the four-run global cap.
- Dashboards, lists, and the TUI name it `user`; JSON `project` is the absolute directory. Copyable Next:/Hint commands preserve `--user` (the NOON-41 contract in internal/cli/guidance.go). Shell completion of job names honors `--user` as it honors `-C`.
- Read-only commands create nothing: `noon --user` without user config exits 0, shows an empty `user` project, and suggests `noon --user add`.

Scope: config loading, validation, and editing for the user project; the global flag and its conflicts; display name, guidance, and completion; scheduler reload of an enabled user project; converting examples/local-dev/housekeeping to user jobs; docs: README, configuration.md (key table, user project section), cli.md (global flags), crontab.md, design.md (jobs, enable and approve, security model).

Tests (critical only): user jobs never appear in or run for a regular project, and repository content can never become user jobs (an authorization boundary); path resolution for the user project; one built-binary test of the CLI flow. Extend existing internal/config, internal/cli, and internal/scheduler tests.

Non-goals: showing user jobs in repository dashboards or job lists; multiple user projects; defaulting `cwd` to `$HOME`; shell `--on-enter` for user jobs; an environment variable for `--user`; approval changes for regular projects.

Dependencies: none. If NOON-49 lands first, its `list_projects` must list an enabled user project as `user`; if this lands first, NOON-49 inherits that through the store.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes before changing them.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `mktemp -d` and the working directory outside any project: `bin/noon --user` exits 0, shows an empty `user` project suggesting `noon --user add`, and creates no files; `bin/noon --user add where "every day at 8am" -- pwd` creates `$XDG_CONFIG_HOME/noon/config.yaml` (file 0600, directory 0700) and enables the user project; `bin/noon --user list --json` shows `where` with `shared: false` and `approval: not_required`; `bin/noon --user run where` exits 0 and prints the canonical user config directory.
- [x] #2 Same environment plus a temp Git repo REPO with its own noon.yaml: `bin/noon -C REPO list --json` has no `where`; `bin/noon -C REPO run where` exits 1 with `not_found`; `bin/noon --user -C REPO list`, `NOON_PROJECT=REPO bin/noon --user list`, `bin/noon --user --config F list`, `bin/noon --user add x --shared -- true`, and `bin/noon --user serve` exit 2; `bin/noon -C "$XDG_CONFIG_HOME/noon" list --json` lists the same jobs as `bin/noon --user list --json`.
- [x] #3 `task cli:test PKG=./internal/scheduler -count=1` exits 0 and covers, with injected time: an enabled user project's scheduled job runs with the user config directory as `cwd`; an enabled regular project in the same state dir never lists or runs it; a broken user job keeps the last valid snapshot and reports the config error.
- [x] #4 `task cli:test PKG=./internal/config -count=1` and `task cli:test PKG=./internal/cli -count=1` exit 0 and cover: a repository's noon.yaml, .git/noon.yaml, or a `noon.yaml` inside the user config directory never contributes user jobs; `bin/noon --user import crontab FILE --write` writes jobs to user config; copyable hints after `--user add` and `--user run NAME --detach` include `--user`; MCP `list_jobs` with `project` set to the user config directory returns user jobs.
- [x] #5 examples/local-dev/housekeeping uses user jobs, and `task cli:test PKG=./integration -- -run '^TestExamplesValidate$' -count=1` exits 0. The README and docs in scope describe the user project, its conflicts, and enablement. `task ci` exits 0.
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
Contract interpretation: explicit --config is rejected when the selected canonical project directory is the user config directory, including selection via -C or MCP. This preserves the settled single loading rule and config.yaml-only source, rather than adding a settings-only override exception.

Modified-file contract expansion: internal/cli/jobs.go owns human list rendering and is necessary to name the user project in list output as required by the settled contract. Approved retaining that focused change; executor disclosed that it edited before requesting expansion, contrary to the required ordering.

Modified-file contract expansion: internal/config/state.go requires the applyFile call site to pass the explicit includeJobs argument after separating validation from job inclusion. Approve the direct call-site update rather than adding a variadic compatibility option solely to avoid touching this file.

Independent verifier passed AC flows and focused suites. Concrete coverage gap: renaming housekeeping/noon.yaml to config.yaml excluded it from TestExamplesValidate discovery. Expand modified-file contract to integration/examples_test.go to include config.yaml in existing example validation, preserving validation of every shipped example.

AC#1: task cli:build; task cli:test PKG=./integration -- -run ^TestUserJobsCLIFlow$ -count=1 -> PASS (executor and independent verifier): isolated built binary checks empty/no writes, add permissions 0700/0600, personal listing and cwd execution.
AC#2: TestUserJobsCLIFlow -> PASS: regular repository cannot list/run personal job, selector/shared/serve conflicts exit 2, and explicit user directory matches --user selection.
AC#3: task cli:test PKG=./internal/scheduler -- -count=1 -> PASS: injected-clock user scheduling/cwd, regular-project isolation, invalid config retains last-good snapshot and reports error.
AC#4: task cli:test PKG=./internal/config -- -count=1; task cli:test PKG=./internal/cli -- -count=1; focused internal/mcp tests -> PASS: hostile source isolation, global validation, import, hints, completion and MCP project selection.
AC#5: task cli:test PKG=./integration -- -run ^TestExamplesValidate$ -count=1 -> PASS after including config.yaml discovery; housekeeping directly validates five jobs. README, configuration, CLI, crontab and design docs updated. task ci -> PASS on implementation commit f2623b572ca299ee47285d5f7e2e6bfba357749b, including security, static checks, full tests, race, build and smoke.
Independent acceptance verification passed. Only concrete correction was restoring renamed example coverage; affected test rerun passed. No tests deleted, skipped or weakened to obtain green; obsolete user-jobs rejection expectations now assert validation and isolation under the new contract. LSP diagnostics on corrected example test clean.
Delivery: f2623b5 fast-forward merged into main. Owned worktree and branch feat/noon-50-user-jobs removed using Worktrunk; exact Herdr workspace wG1 disappeared. No retained checkout/workspace, no push. No blocker or next resumable implementation step.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented personal user-config jobs with --user, source isolation, secure editing, scheduler support, guidance/completion, tests and documentation. Merged f2623b5 into main after independent acceptance verification and passing task ci; worktree/branch/workspace cleaned up.
<!-- SECTION:FINAL_SUMMARY:END -->
