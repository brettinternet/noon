---
id: NOON-6
title: Import crontabs with preview and faithful semantics
status: Done
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 18:23'
labels:
  - config
  - integration
  - reviewed
dependencies:
  - NOON-20
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-5 -
    Job-semantics-follow-cron-and-launchd-expectations.md
modified_files:
  - internal/cronimport/**
  - internal/config/**
  - internal/app/**
  - internal/cli/**
  - docs/crontab.md
  - README.md
priority: medium
type: feature
ordinal: 1900
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users move existing crontab entries into Noon, preview first, with cron's meaning preserved and clear refusals for anything that would change it.

Read first: docs/design.md#schedules and #integrations (Crontab import), decision-5.

Scope:
- New internal/cronimport: `Parse(r io.Reader) ([]Job, error)`. It reports every bad line at once, each as `line N: message`.
- Accepted: blank lines and comments; `NAME=value` environment lines (strip one pair of matching single or double quotes), applied as `env` to the entries after them; five-field entries; and the macros `@hourly`, `@daily`, `@midnight`, `@weekly`, `@monthly`, `@yearly`, `@annually`. The rest of the line, trimmed, becomes a string `run`.
- Refused, with the reason and the line number:
  - `@reboot`;
  - any `%` in a command (cron turns it into newlines and stdin);
  - non-empty `MAILTO` (`MAILTO=""` is accepted and ignored);
  - `SHELL` other than `/bin/sh`;
  - `CRON_TZ` and `TZ` (hint: set the project timezone);
  - `NOON_*` names;
  - schedules `schedule.Parse` rejects.
- Faithful defaults: every imported job sets `missed: skip` (cron never catches up) and `timeout: none` (cron has no timeout), each with a YAML comment saying so. Document that Noon never overlaps the same job.
- Names, which must pass the job-name rule `^[a-z][a-z0-9_-]{0,63}$`: take the basename of the command's first word, lowercase it, replace runs of characters outside `[a-z0-9_-]` with `-`, and trim leading and trailing `-` and `_`. Use `cron-LINE` when the result is empty, and prefix `cron-` when it starts with a digit. Duplicates within the import and collisions with existing project jobs get `-2`, `-3`, and so on. Truncate the stem so the whole name, suffix included, fits 64 characters.
- `noon import crontab FILE|-`: print a preview of the `jobs:` YAML with a `# crontab line N` comment per job, and write nothing. `--write` adds all jobs to `.noon.yaml` using the YAML editing functions from the add task, or writes nothing if any line failed. Like `noon add`, it is refused when an explicit config file is selected. JSON: `{"schema_version":1,"jobs":[{"name","schedule","run","env_keys","line"}],"written":bool}`.
- Bare `noon import` lists importers; bare `noon import crontab` prints usage with `crontab -l | noon import crontab -` and exits 0.
- Add docs/crontab.md with the supported and refused features.

Non-goals: reading system crontabs or `/etc/cron.d`, user columns, anacron, live crontab sync, an overlap-allowing mode.

Tests:
- internal/cronimport `TestImportCrontab` (table): macros, env lines with quotes, each refusal with its line number, all errors reported together, naming and dedupe (including `_backup` becoming `backup`, `7z` becoming `cron-7z`, and two different commands whose names normalize to the same 64-character stem), and the preview loading with `config.Load`.
- internal/cronimport `FuzzImportCrontab`: never panics; successful output always loads with `config.Load`.
- internal/cli `TestImportWrite`: `--write` adds every job or none, and preserves existing `.noon.yaml` comments.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `task cli:test -- -run '^(TestImportCrontab|TestImportWrite)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [x] #2 `mise exec go -- go test ./internal/cronimport -run '^$' -fuzz '^FuzzImportCrontab$' -fuzztime 30s` exits 0
- [x] #3 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf '# nightly\n0 2 * * * /usr/local/bin/backup.sh\n@reboot echo hi\n' | env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" import crontab -; echo "exit=$?"` names line 3 and `@reboot`, prints `exit=1`, and leaves `$noon_ac_project` empty
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
1. Reuse manifest editing and schedule validation for faithful crontab parsing, preview, and atomic import. 2. Add critical parser/CLI regressions, fuzz coverage, and documentation. 3. Run acceptance commands and CI, review once, commit, merge locally, and remove the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Continuation: resumed the same loop claim and adopted feat/noon-6-crontab using the creation receipt and prior session transcript; executor and reviewer finished. Single review found two parser defects (assignment whitespace and numeric commands); both fixed with a regression. No tests removed or weakened; changes stay within the modified-file contract. AC#1: task cli:test -- -run '^(TestImportCrontab|TestImportWrite)$' -count=1 -v -> exit 0; both tests print PASS, including review regressions. AC#2: mise exec go -- go test ./internal/cronimport -run '^$' -fuzz '^FuzzImportCrontab$' -fuzztime 30s -> exit 0, 217397 executions. AC#3: task cli:build plus the specified stdin fixture under env -i and isolated HOME/project -> line 3: @reboot, exit=1, project empty. The shell guard rejects the dynamic-variable command form; equivalent literal mktemp paths /tmp/noon-ac-home.uN9bWC and /tmp/noon-ac-project.2voehf were used. Next: final CI, commit, local integration and cleanup.

Delivery: c5e42905574092b7d5c60e0f413319b1e8e8c891 (feat: import crontabs with faithful defaults) passed task check:staged and task ci on the committed implementation, including security, static analysis, all tests, race tests and smoke. Fast-forward merged into main. Worktrunk removed the verified adopted worktree and branch; its idle-shell Herdr workspace was closed by the post-remove hook and absence verified. Single review complete; both concrete findings fixed and focused checks rerun. No remaining blocker or resumable implementation step.

Review (post-merge): parser (env lines, quote stripping, macros, refusals with line numbers, aggregated errors), naming/dedupe/truncation, faithful missed/timeout defaults, preview, all-or-nothing --write with comment preservation, explicit-config refusal, and JSON shape match the contract; DOM/DOW OR semantics verified on `0 2 1 * 1`. Fixed in 579c79b: (1) a command whose basename is `true`, `false`, or `null` produced an unquoted YAML key, so the preview did not load and --write failed with "job names must be strings" (the same bug made `noon add null ...` fail); job keys are now quoted when YAML would not read them as strings. (2) the YAML encoder emits the string `<<` as a plain merge key, so `@daily <<` produced `run: !!merge <<`, which does not load (found by a 20s FuzzImportCrontab run, also affects `noon add`); encodeJob now quotes such scalars. Regressions: TestImportCrontab preview case with a `true` command and the saved fuzz corpus entry; both fail before the fix. Low, not fixed: the six-field heuristic only triggers when the first five fields fail to parse, so `0 */5 * * * * cmd` imports with run `* cmd` (same as cron would run); env values under `env:` render with 8-space indent. AC1-AC3 rerun and pass; task ci passes.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented preview-first crontab import with faithful cron defaults, aggregated refusals, collision-safe naming, JSON output, and atomic comment-preserving writes. Acceptance tests, 30-second fuzzing, isolated refusal fixture and full CI pass. Committed as c5e4290, merged to main, and removed the worktree/branch and Herdr workspace.
<!-- SECTION:FINAL_SUMMARY:END -->
