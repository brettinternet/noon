---
id: NOON-88
title: Compose private jobs from .git/noon.d fragments
status: done
type: feature
labels: [config, security]
created: 2026-10-29T17:08:27Z
updated: 2026-10-29T17:41:33Z
---

Outcome: a repository can compose its private job list from several files, so job sets kept in dotfiles can be symlinked into each repository independently and mixed with repository-specific private jobs. `.git/noon.yaml` stays a normal, editable file and becomes optional.

Context: today reusing a job set means symlinking the whole `.git/noon.yaml`, which can't be mixed with other job sets or with `noon add` (which refuses symlinked files). Supersedes the NOON-84 templates/`use:` proposal: the dotfiles directory is the template library and a symlink is the "use", with no new schema, no user-config-to-repository coupling, and fragments that are ordinary Noon config files.

Behavior:
- Private fragments: every `*.yaml` entry directly in `noon.d/` beside the private file (`.git/noon.d/`, or the main checkout's common Git dir for linked worktrees) is a private config source. Hidden entries, other extensions, and subdirectories are ignored. A missing directory is fine. No `.git` means no fragments, as with `.git/noon.yaml`.
- `.git/noon.yaml` is not required; fragments alone work. It's still where `noon add` writes and where the `{enabled: false}` toggle lives.
- Load order: user settings → `noon.yaml` → `.git/noon.d/*.yaml` (lexical) → `.git/noon.yaml` → env → flags. `--config`/`NOON_CONFIG` and `--user` ignore fragments.
- Fragment keys: only `version`, `vars_file`, `definitions`, `jobs`. Other keys are errors naming the file where they belong (`timezone`/`default_agent` → `.git/noon.yaml`), so file order never decides a setting.
- Each fragment follows the existing per-file rules: 1 MiB limit, strict YAML, anchors within the file, its own `vars_file` resolved from the project root (so a dotfiles fragment with `vars_file: .noon.vars` picks up each repo's values). Dangling symlinks are errors, as today.
- Trust: fragment jobs are private (`shared: false`, no approval).
- Collisions: the same job name in two fragments, or in a fragment and `.git/noon.yaml`, is an error naming both files, except a `{enabled: false}` toggle in `.git/noon.yaml`, which disables the fragment job. A fragment job replaces a shared `noon.yaml` job of the same name and counts as an override, like `.git/noon.yaml` does today.
- Editing: `noon add` never writes fragments; adding a name that exists in a fragment is a collision error. `noon remove` of a fragment job errors naming the fragment (remove the link instead).
- Reporting: `noon config` lists each fragment in Files in load order; `list --json` `source` is the fragment path as linked (not the resolved target).
- Reload: `noon serve` picks up added, removed, and edited fragments on its normal reload.

Recommendations settled for this task:
1. Home paths in private files. Leading `~/` (and bare `~`) expands to the user's home directory in path fields (`cwd`, `env_file` entries, `prompt_file`, `vars_file`) of private sources only: fragments, `.git/noon.yaml`, and the user project's `config.yaml`. Shared `noon.yaml` and `--config` files keep `~` literal (committed paths should be repo-relative; a per-user home path in a shared file is a smell). `~user` forms stay unsupported. `run` is unchanged: argv is literal, and the string form already gets shell expansion of `~` and `$HOME`. This makes dotfiles fragments portable across machines without hardcoded `/Users/NAME` paths. Fragment-relative path resolution is rejected: it would break the project-root rule that makes `.env` mean each repo's own file.
2. Symlinks back into the working tree. A private config file (fragment or `.git/noon.yaml`) whose resolved target lies inside the project root or the main checkout root, but outside the Git common dir, is an error: committed content must not run unapproved through a link. This also closes the same gap for an existing symlinked `.git/noon.yaml`; call it out in the changelog.
3. Edits to a shared dotfiles fragment change every linked repository at the next reload. Accepted as the point of the feature; no extra mechanism.

Non-goals: fragments for shared `noon.yaml` or `--config`; fragments for the user project (`~/.config/noon/noon.d/`; follow up only if needed); include/use directives or cross-file YAML references; field-level merging between files; a `noon link`/`use` command; `~` expansion in shared files or in `run`, `env`, or prompts; JSON schema changes.

Modified-file contract: internal/config/{config.go,jobs.go,vars.go,edit.go} and their tests; internal/app/{inspect.go,manage.go} and internal/cli/{root.go,manage.go} only as needed for source/Files reporting and remove/add errors; README.md, docs/{configuration.md,design.md,cli.md}, CHANGELOG.md, internal/skill/SKILL.md; NOON-84 note.

Dependencies: none.

## Acceptance criteria

- [x] mise exec -- task cli:test PKG="./internal/config ./internal/app ./internal/cli" -- -count=1 -> exit 0. Tests cover: jobs from two .git/noon.d fragments load as private (no approval) with no .git/noon.yaml present; a linked worktree reads the common-dir noon.d; duplicate names across fragments, and between a fragment and .git/noon.yaml, error naming both files, while a .git/noon.yaml {enabled: false} toggle disables a fragment job; a fragment job overrides a same-named noon.yaml job; timezone in a fragment errors naming .git/noon.yaml; a fragment or .git/noon.yaml symlink resolving into the working tree errors; ~/ expands in prompt_file, env_file, cwd, and vars_file of private files and stays literal in shared noon.yaml.
  Evidence: AC#1: task cli:test config/app/cli -count=1 -> exit 0 on 91c64f8 (TestPrivateFragments)
- [x] mise exec -- task cli:build -> exit 0. With HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one mktemp -d, two temp Git repos, and two fragment files under $HOME/dotfiles/noon whose prompt_file/env_file use ~/ paths: link fragment A into both repos and fragment B into one; bin/noon config lists the fragments; bin/noon list --json in each repo shows the expected jobs with source set to the link path and approval not_required; bin/noon run JOB exits 0 in both; bin/noon add in a repo with only fragments creates a real .git/noon.yaml; editing fragment A while bin/noon serve runs updates both repos within one reload; record commands and output in task notes.
  Evidence: AC#2: task cli:build exit 0; isolated e2e (/tmp/noon88-ac/ac.sh): config/list/run/add/serve reload all as expected, see notes
- [x] mise exec -- task ci -> exit 0. README, docs/configuration.md, docs/design.md, docs/cli.md, CHANGELOG.md, and internal/skill/SKILL.md describe only the implemented fragment, ~ expansion, and symlink-guard behavior.
  Evidence: AC#3: task ci exit 0 on 91c64f8; README, configuration, design, cli, SKILL, CHANGELOG updated

## Definition of done

- [x] task ci passes on the final commit
  Evidence: task ci exit 0 on 91c64f8
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
  Evidence: AC#1-3 evidence lines in notes
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
  Evidence: Deviations justified in notes
- [x] No test was deleted, skipped, or weakened
  Evidence: Only jobs_test orphan-toggle message updated for extended rule
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
  Evidence: Docs reviewed against implementation

## Notes

Implemented on branch noon-88-config-fragments (commits 5b685e0, e3da92a, 91c64f8), fast-forwarded into main.

AC#1: `mise exec -- task cli:test PKG="./internal/config ./internal/app ./internal/cli" -- -count=1` -> ok config, app, cli (exit 0) on 91c64f8. TestPrivateFragments (internal/config/config_test.go) covers: fragments with no .git/noon.yaml load private; a linked worktree reads the common-dir noon.d; fragment/fragment and fragment/.git/noon.yaml collisions name both files; a .git/noon.yaml {enabled: false} toggle disables a fragment job; a fragment job overrides noon.yaml; timezone in a fragment names .git/noon.yaml; fragment, noon.d directory, and .git/noon.yaml links into the working tree error; ~/ expands in cwd, prompt_file, env_file, and vars_file of fragments and stays literal in shared noon.yaml.

AC#2: `mise exec -- task cli:build` -> exit 0. Script /tmp/noon88-ac/ac.sh with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, NOON_STATE_DIR under one mktemp -d; fragments greet.yaml (env_file: ~/dotfiles/noon/greet.env, vars_file per repo) and review.yaml (prompt_file: ~/dotfiles/noon/prompts/review.md) under $HOME/dotfiles/noon; greet linked into repos one and two, review into one only. Observed:
- `noon -C one config` lists .git/noon.d/greet.yaml and review.yaml; two lists greet.yaml (exit 0).
- `list --json`: one -> greet {source: one/.git/noon.d/greet.yaml, shared: false, approval: not_required, timeout: 2m0s from one's .noon.vars}, review {source: one/.git/noon.d/review.yaml, not_required}; two -> greet {source: two/.git/noon.d/greet.yaml, not_required, timeout 5m0s default}.
- `run greet` in one -> "hello from greet in one", exit 0; `run review` -> prompt from ~/dotfiles read, exit 0; `run greet` in two -> "hello from greet in two", exit 0.
- `noon -C two add extra --sh true` -> "Added extra to .git/noon.yaml (private)", exit 0; .git/noon.yaml is a regular 0600 file.
- `add greet` in two -> exit 1 "job \"greet\" is already defined in .git/noon.d/greet.yaml"; `remove greet` in one -> exit 1 "Noon does not edit fragments" with the toggle hint.
- Enabled both, ran `noon serve`, appended a minutely `tick` job to dotfiles greet.yaml at 11:23:51; both repos had tick.txt by 11:24:01 and history shows tick trigger=schedule status=succeeded.

AC#3: `mise exec -- task ci` -> exit 0 on 91c64f8 (lint, tests, build, smoke). Docs updated: README (approval table), docs/configuration.md (file order, Fragments section, keys table, ~ expansion in private files, local overrides), docs/design.md (jobs merge, approval, privacy, security table), docs/cli.md (remove refuses fragments), internal/skill/SKILL.md (temporary jobs with fragments), CHANGELOG.md via task changelog.

Contract deviations:
- internal/scheduler/serve.go: serve reloads only when stamped files change, so configPaths stamps the noon.d directory (adds/removals) and each fragment (link target edits). Without it, AC#2's reload fails.
- internal/app/run.go: ~ expansion makes private prompt_file absolute; RunSpec now joins only relative paths onto the project.
- internal/cli/verify.go, internal/cli/import.go: verifyRawJobsUnchanged and JobFromSource take a private flag so add/remove/import verify private files with the same ~ expansion as load.
- Test files (jobs_test, serve_test, vars_test, manage_test, edit_test): JobFromSource signature update only; jobs_test's orphan-toggle message now says "requires a shared or .git/noon.d job" (rule extended, not weakened).
- A pre-existing .git/noon.yaml symlink that resolves into the working tree now errors (noted in docs).

## Summary

Private jobs now compose from .git/noon.d/*.yaml fragments (shared by linked worktrees), loaded between noon.yaml and .git/noon.yaml. Private files expand leading ~ in job paths and vars_file; private job names must be unique; links into the working tree are refused; serve reloads on fragment changes; add/remove never edit fragments.
