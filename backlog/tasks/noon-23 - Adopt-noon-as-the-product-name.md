---
id: NOON-23
title: Adopt noon as the product name
status: In Progress
assignee: []
created_date: '2026-10-16 04:16'
updated_date: '2026-10-16 04:30'
labels:
  - config
  - cli
  - ops
dependencies:
  - NOON-17
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
- [x] #1 `mise exec go -- go list -m` prints `github.com/brettinternet/noon`; after `task cli:build`, `bin/noon --help | sed -n 2p` prints `   noon - Repo-local scheduling, without a platform`
- [x] #2 With `noon_ac_home=$(mktemp -d); mkdir "$noon_ac_home/p"`, `env -i PATH="$PATH" HOME="$noon_ac_home" NOON_TIMEZONE=UTC BUMP_STATE_DIR=/nonexistent bin/noon -C "$noon_ac_home/p" --json config` exits 0 and reports `"timezone":"UTC"` and `"state_dir":"<noon_ac_home>/.local/state/noon"`, so old BUMP_* variables are ignored
- [x] #3 With the same home, `env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_home/p" init` exits 0 and `ls -A "$noon_ac_home/p"` prints only `noon.yaml`. Then `printf 'version: 1\njobs:\n  tests:\n    run: [env]\n' > "$noon_ac_home/p/noon.yaml"; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_home/p" run tests | grep -E '^(NOON|BUMP)_'` prints `NOON_PROJECT=…`, `NOON_JOB=tests`, `NOON_TRIGGER=manual`, and `NOON_RUN_ID=…`, with no `BUMP_` line, and `test -f "$noon_ac_home/.local/state/noon/noon.db"` exits 0
- [x] #4 `task smoke && task docker:smoke` exits 0, and `docker image inspect noon:dev --format ok` prints `ok`
- [x] #5 `backlog config list` shows `projectName: noon` and `taskPrefix: NOON`; `backlog task list --plain` lists exactly 22 tasks, every one `NOON-N` (NOON-23 is this task); `backlog task NOON-17 --plain` shows the dependency NOON-3; `backlog doctor` reports no duplicate IDs and no dependency problems; `task backlog:next` prints a NOON- task or nothing
- [x] #6 Outside this task's own record and the single README migration line (both must name the old identifiers), the repo-wide scan `rg -n -i --hidden -g '!.git/' -g '!.worktrees/' -g '!backlog/tasks/noon-23 - *' bump . | rg -v '^\./\.taskfiles/setup\.yaml:[0-9]+:\s+cmd: mise upgrade --bump --local$' | rg -v '^\./README\.md:[0-9]+:Existing local users:'` prints nothing, and `fd -H -I -i bump --exclude .git .` prints nothing (no bump-named files, including bin/bump; this task's file is `noon-23 - Adopt-noon-as-the-product-name.md`)
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] #4 No test was deleted, skipped, or weakened
- [x] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Mechanically rename product identifiers, paths, and backlog IDs in the primary checkout using the explicitly approved exceptions. 2. Run all acceptance commands and task ci, and review the rename for missed identifiers or behavior changes. 3. Commit intended changes and completion evidence, verify final commit, and release the claim.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
User explicitly approved working directly on main instead of a worktree and the task-specific mechanical backlog rewrite exception because Backlog.md cannot re-prefix existing tasks. No worktree merge or cleanup is needed.

User approved exempting the one required README migration line from AC#6. Backlog task IDs and filenames were mechanically renamed under the approved exception; all other task fields preserved apart from product identifiers. The existing Worklease resource remains BUMP-23 through release to preserve claim continuity.

User approved correcting AC#5 expected task count to 22: git ls-tree HEAD backlog/tasks confirms only 22 source records existed, with ID 9 previously removed. User authorized starting the existing Colima VM for container acceptance and stopping it afterwards. Initial task ci passed; Docker check retry follows daemon startup.

AC#1: mise exec go -- go list -m; task cli:build; bin/noon --help | sed -n 2p -> github.com/brettinternet/noon and the exact required noon usage line, exit 0.
AC#2: sh /tmp/noon-23-acceptance.sh executes the specified env -i config invocation with NOON_TIMEZONE=UTC and BUMP_STATE_DIR=/nonexistent -> UTC and the isolated home/.local/state/noon; exit 0.
AC#3: the same script executes the specified init, ls, env job and database test -> only noon.yaml; NOON_PROJECT, NOON_JOB=tests, NOON_TRIGGER=manual, NOON_RUN_ID=1 and no BUMP_ output; noon.db exists; exit 0. Inline shell was rejected by the tool parser, so the exact commands ran from the script.
AC#4: task smoke && task docker:smoke && docker image inspect noon:dev --format ok -> exit 0 and ok after authorized Colima startup. Colima stopped afterwards.
AC#5: backlog config list; backlog task list --plain; backlog task NOON-17 --plain; backlog doctor; task backlog:next -> noon/NOON config, all 22 original tasks renamed (approved correction), NOON-3 dependency preserved, no duplicates/dependency problems, next NOON-18.
AC#6: specified rg scan with the approved README migration-line exclusion -> no matches; fd -H -I -i bump --exclude .git . -> no output. Stale bin/bump trashed. git diff --check clean.
Validation: task ci passed (security, vet, Staticcheck, full tests, race, isolated smoke); LSP diagnostics clean for internal/cli/root.go. No tests added, removed, skipped, or weakened: assertions only renamed. All modified paths remain in contract. Independent review pending; next commit and final-commit CI.

Single independent review found one concrete defect: the README directory-only migration left bump.db unread and could collide with existing log IDs. Corrected the same migration line to require stopped processes, backup, absent destinations, database/sidecar renames, and intact logs. sh /tmp/noon-23-migration.sh built the old HEAD binary, recorded run 1, migrated its real state, then verified noon history and logs preserve run 1 and a new run succeeds as ID 2. Exit 0. No runtime behavior changed; no second general review needed.
<!-- SECTION:NOTES:END -->
