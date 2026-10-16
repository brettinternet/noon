---
id: BUMP-6
title: Import crontabs with preview and faithful semantics
status: To Do
assignee: []
created_date: '2026-10-16 00:18'
updated_date: '2026-10-16 02:03'
labels:
  - config
  - integration
dependencies:
  - BUMP-20
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
Outcome: users move existing crontab entries into Bump, preview first, with cron's meaning preserved and clear refusals for anything that would change it.

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
  - `BUMP_*` names;
  - schedules `schedule.Parse` rejects.
- Faithful defaults: every imported job sets `missed: skip` (cron never catches up) and `timeout: none` (cron has no timeout), each with a YAML comment saying so. Document that Bump never overlaps the same job.
- Names, which must pass the job-name rule `^[a-z][a-z0-9_-]{0,63}$`: take the basename of the command's first word, lowercase it, replace runs of characters outside `[a-z0-9_-]` with `-`, and trim leading and trailing `-` and `_`. Use `cron-LINE` when the result is empty, and prefix `cron-` when it starts with a digit. Duplicates within the import and collisions with existing project jobs get `-2`, `-3`, and so on. Truncate the stem so the whole name, suffix included, fits 64 characters.
- `bump import crontab FILE|-`: print a preview of the `jobs:` YAML with a `# crontab line N` comment per job, and write nothing. `--write` adds all jobs to `.bump.yaml` using the YAML editing functions from the add task, or writes nothing if any line failed. Like `bump add`, it is refused when an explicit config file is selected. JSON: `{"schema_version":1,"jobs":[{"name","schedule","run","env_keys","line"}],"written":bool}`.
- Bare `bump import` lists importers; bare `bump import crontab` prints usage with `crontab -l | bump import crontab -` and exits 0.
- Add docs/crontab.md with the supported and refused features.

Non-goals: reading system crontabs or `/etc/cron.d`, user columns, anacron, live crontab sync, an overlap-allowing mode.

Tests:
- internal/cronimport `TestImportCrontab` (table): macros, env lines with quotes, each refusal with its line number, all errors reported together, naming and dedupe (including `_backup` becoming `backup`, `7z` becoming `cron-7z`, and two different commands whose names normalize to the same 64-character stem), and the preview loading with `config.Load`.
- internal/cronimport `FuzzImportCrontab`: never panics; successful output always loads with `config.Load`.
- internal/cli `TestImportWrite`: `--write` adds every job or none, and preserves existing `.bump.yaml` comments.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^(TestImportCrontab|TestImportWrite)$' -count=1 -v` exits 0 and prints `--- PASS` for both, covering every case listed under Tests
- [ ] #2 `mise exec go -- go test ./internal/cronimport -run '^$' -fuzz '^FuzzImportCrontab$' -fuzztime 30s` exits 0
- [ ] #3 `task cli:build && bump_ac_home=$(mktemp -d) && bump_ac_project=$(mktemp -d) && printf '# nightly\n0 2 * * * /usr/local/bin/backup.sh\n@reboot echo hi\n' | env -i PATH="$PATH" HOME="$bump_ac_home" bin/bump -C "$bump_ac_project" import crontab -; echo "exit=$?"` names line 3 and `@reboot`, prints `exit=1`, and leaves `$bump_ac_project` empty
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
