---
id: BUMP-23
title: Adopt noon as the product name
status: To Do
assignee: []
created_date: '2026-10-16 04:16'
updated_date: '2026-10-16 04:16'
labels:
  - config
  - cli
  - ops
dependencies:
  - BUMP-17
documentation:
  - README.md
  - docs/development.md
  - docs/configuration.md
  - docs/design.md
modified_files:
  - go.mod
  - cmd/**
  - internal/**
  - scripts/smoke.sh
  - Dockerfile
  - Taskfile.dist.yaml
  - .taskfiles/cli.yaml
  - cliff.toml
  - .gitignore
  - bump.yaml
  - noon.yaml
  - README.md
  - AGENTS.md
  - CLAUDE.md
  - docs/**
  - backlog/**
priority: high
type: chore
ordinal: 750
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the product is named noon everywhere: binary, Go module, config files, user config and state paths, environment variables, container image, docs, agent instructions, and the backlog (including task IDs, NOON-N). After this task, "bump" survives only in Git history and in mise's own `mise upgrade --bump` flag.

Context: the GitHub repo is already brettinternet/noon, and origin already points there. There are no tags or releases, so this is a clean break with no compatibility layer.

Preconditions (if any is unmet, record it in the notes and stop):
- BUMP-17 is Done and merged to main, and no other task is In Progress.
- `git worktree list` shows only the primary checkout, and `git branch` shows only main. Backlog's check_active_branches reads task files from other local branches, so stale bump-N files there would show up again as duplicates.
- Work directly in the primary checkout on main, not in a worktree. This task renames the backlog task files, which hold the shared claim state. Claim it as BUMP-23. After the rename it is NOON-23.

Rename map (old → new):
| Old | New |
|---|---|
| command `bump`, `cmd/bump/`, `bin/bump`, urfave root `Name`, help/usage text | `noon`, `cmd/noon/`, `bin/noon` |
| module `github.com/brettinternet/bump` and every import | `github.com/brettinternet/noon` |
| `bump.yaml` / `.bump.yaml`: discovery, `init`, `init --local`, errors and hints, help, tests, the repo's own `bump.yaml`, `.gitignore` | `noon.yaml` / `.noon.yaml` |
| `.bump/` (comment in the repo's bump.yaml) | `.noon/` |
| user config `$XDG_CONFIG_HOME/bump/config.yaml`, `~/.config/bump/` | `.../noon/...` |
| state `$XDG_STATE_HOME/bump`, `~/.local/state/bump`, database `bump.db` | `.../noon`, `noon.db` |
| flag env vars `BUMP_PROJECT`, `BUMP_CONFIG`, `BUMP_TIMEZONE`, `BUMP_STATE_DIR` | `NOON_*` |
| job env vars `BUMP_PROJECT/JOB/TRIGGER/RUN_ID/LAST_SUCCESS_AT/AGENT`, designed `BUMP_SCHEDULED_AT/EVENT_FILE`, reserved prefix `BUMP_` | `NOON_*`, reserved `NOON_` |
| Dockerfile: `/out/bump`, `./cmd/bump`, `/usr/local/bin/bump`, user/group `bump`, `ENV BUMP_STATE_DIR`, `ENTRYPOINT ["bump"]` | `noon` |
| Taskfile.dist.yaml image `bump:dev` and descs; .taskfiles/cli.yaml build/run paths and descs | `noon` |
| scripts/smoke.sh `bump_binary`, `bump_smoke_dir`, `.bump-smoke-owner`, `bump-smoke.XXXXXX` | `noon_*`, `.noon-smoke-owner`, `noon-smoke.XXXXXX` |
| docs/development.md acceptance convention `bump_ac_home`, `bin/bump`, `BUMP_*` | `noon_ac_home`, `bin/noon`, `NOON_*` |
| docs/design.md extension executables `bump-NAME` | `noon-NAME` |
| cliff.toml changelog header and compare/commit URLs | noon |
| prose "Bump"/"bump" in README.md (title included), AGENTS.md, CLAUDE.md, docs/*.md, the `init` template comment | "Noon" in prose, `noon` for commands and paths |
| backlog/config.yml `project_name: "bump"`, `task_prefix: "BUMP"` | `noon`, `NOON` |
| `backlog/tasks/bump-N - *.md` (use `git mv`), frontmatter `id: BUMP-N`, every `BUMP-N` dependency and cross-reference, product-name text in tasks of every status, drafts, and decisions | `noon-N - *.md`, `NOON-N`, noon |

Backlog exception: Backlog.md 1.52 cannot re-prefix existing tasks, and it reports taskPrefix as read-only. So this task alone edits backlog/config.yml and the backlog Markdown files directly, using `git mv` plus mechanical substitution. Keep every other field byte-for-byte. Record the exception in Implementation Notes. After the rename, use the CLI on NOON-23 for status, notes, and checks.

Compatibility: clean break. Don't read old files, env vars, or directories as a fallback. Add no deprecation warnings and no `bump` alias or symlink. README gets one manual line for existing local users: move `~/.local/state/bump` and `~/.config/bump` to the matching `noon` paths.

Tests: update the existing tests in place for the new names, without weakening any assertion. Add no new tests, because this is a rename only.

Also: trash the stale local build artifact `bin/bump`.

Non-goals: any behavior change other than names; rewriting Git history or past commit messages; the GitHub rename (already done); adding a CHANGELOG.md; the `mise upgrade --bump --local` line in .taskfiles/setup.yaml (that is mise's flag); renaming the local checkout directory.

After merge (manual user steps, outside this task): rename `/Users/brett/dev/me/bump` to `/Users/brett/dev/me/noon`, run `git worktree repair` if any worktrees exist, run `mise trust` and `task init` in the new path, update the Herdr workspace, and move `~/.local/state/bump` and `~/.config/bump` if they exist.

Modified-file contract: the modified_files list (renames included) plus the whole backlog/ tree, as described above. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `mise exec go -- go list -m` prints `github.com/brettinternet/noon`; after `task cli:build`, `bin/noon --help | sed -n 2p` prints `   noon - Repo-local scheduling, without a platform`
- [ ] #2 With `noon_ac_home=$(mktemp -d); mkdir "$noon_ac_home/p"`, `env -i PATH="$PATH" HOME="$noon_ac_home" NOON_TIMEZONE=UTC BUMP_STATE_DIR=/nonexistent bin/noon -C "$noon_ac_home/p" --json config` exits 0 and reports `"timezone":"UTC"` and `"state_dir":"<noon_ac_home>/.local/state/noon"`, so old BUMP_* variables are ignored
- [ ] #3 With the same home, `env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_home/p" init` exits 0 and `ls -A "$noon_ac_home/p"` prints only `noon.yaml`. Then `printf 'version: 1\njobs:\n  tests:\n    run: [env]\n' > "$noon_ac_home/p/noon.yaml"; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_home/p" run tests | grep -E '^(NOON|BUMP)_'` prints `NOON_PROJECT=…`, `NOON_JOB=tests`, `NOON_TRIGGER=manual`, and `NOON_RUN_ID=…`, with no `BUMP_` line, and `test -f "$noon_ac_home/.local/state/noon/noon.db"` exits 0
- [ ] #4 `task smoke && task docker:smoke` exits 0, and `docker image inspect noon:dev --format ok` prints `ok`
- [ ] #5 `backlog config list` shows `projectName: noon` and `taskPrefix: NOON`; `backlog task list --plain` lists exactly 23 tasks, every one `NOON-N` (NOON-23 is this task); `backlog task NOON-17 --plain` shows the dependency NOON-3; `backlog doctor` reports no duplicate IDs and no dependency problems; `task backlog:next` prints a NOON- task or nothing
- [ ] #6 Outside this task's own record (it must name the old identifiers), the repo-wide scan `rg -n -i --hidden -g '!.git/' -g '!.worktrees/' -g '!backlog/tasks/noon-23 - *' bump . | rg -v '^\./\.taskfiles/setup\.yaml:[0-9]+:\s+cmd: mise upgrade --bump --local$'` prints nothing, and `fd -H -I -i bump --exclude .git .` prints nothing (no bump-named files, including bin/bump; this task's file is `noon-23 - Adopt-noon-as-the-product-name.md`)
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
