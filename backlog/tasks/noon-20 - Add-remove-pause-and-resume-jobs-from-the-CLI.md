---
id: NOON-20
title: 'Add, remove, pause, and resume jobs from the CLI'
status: To Do
assignee: []
created_date: '2026-10-16 01:41'
updated_date: '2026-10-16 02:03'
labels:
  - config
  - cli
dependencies:
  - NOON-5
  - NOON-16
documentation:
  - docs/design.md
  - docs/development.md
  - >-
    backlog/decisions/decision-2 -
    Repository-defined-jobs-run-unattended-only-after-approval-of-their-current-definition.md
modified_files:
  - internal/config/**
  - internal/store/**
  - internal/scheduler/**
  - internal/app/**
  - internal/cli/**
  - README.md
  - docs/configuration.md
priority: high
type: feature
ordinal: 1300
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users create and manage jobs without hand-editing YAML. One command adds a scheduled shell, argv, or agent-prompt job, previews its next run, and enables scheduling. Existing files keep every byte the edit does not touch. Pause and resume control background runs without touching files.

Read first: docs/design.md#experience (the first-run transcript is the target output), #jobs-and-configuration, and #enable-and-approve; decision-2.

Scope:
- `noon add NAME [SCHEDULE] ACTION [flags]`. ACTION is exactly one of `-- ARGV...` (list `run`), `--sh STRING` (string `run`), `--prompt TEXT`, or `--prompt-file PATH`. Flags: `--agent NAME`, `--description TEXT`, `--timeout DURATION`, `--shared` (write `noon.yaml`; default `.noon.yaml`), and one of `--in DURATION` or `--at TIME` (queue a run after writing).
- SCHEDULE is one optional positional argument. Omit it for a manual job.
- Bare `noon add` prints these examples and exits 0:
```
noon add tests "every day at 2am" -- go test ./...
noon add lint "every 30m" --sh 'golangci-lint run ./...'
noon add review "weekdays at 9am" --agent claude --prompt "Review open PRs and list risks"
noon add notes --agent claude --prompt-file prompts/notes.md
noon add check-ci --in 2h --agent claude --prompt "Check CI and fix failures"
```
- Validation before writing: name pattern; schedule and duration parsing with suggestions (usage errors, exit 2). The same name in the target file is an `exists` error with hint `noon remove NAME`. The same name in the other file is allowed, with a note that the local job overrides the shared one. A target file that does not load fails with its `file:line` error and is not touched.
- Explicit config: when `--config` or `NOON_CONFIG` selects a file, `add` and `remove` fail with usage error code `explicit_config` and the hint to edit that file directly. Nothing is written.
- Editing (internal/config owns `AddJob(src []byte, name string, job Job) ([]byte, error)` and `RemoveJob(src []byte, name string) ([]byte, error)`; `noon import crontab` reuses them later). Splice text; never re-encode the document. Use the parsed yaml.Node only to find line positions.
  - Supported shapes: an empty or missing file (write `version: 1` and a `jobs:` block, 2-space indentation), a file without `jobs` (append a `jobs:` block at the end), or `jobs` as a non-empty block mapping. Anything else, such as flow style or `jobs: {}`, fails with code `config_edit` and the hint to edit the file by hand.
  - The jobs block runs from the line after `jobs:` to the line before the next top-level key, or to the end of the file. It excludes trailing lines that are blank or are comments starting in column 1.
  - Add renders the job with the YAML encoder, so quoting is correct, at the indentation the existing job keys and fields use, and inserts it after the last line of the jobs block. It adds one blank line before the job when the current last job is preceded by a blank line. Inserted lines use the file's line ending (LF or CRLF).
  - Remove deletes from the job's first head-comment line (comment lines directly above its key at the same indentation) through the line before the next job's first head-comment line or key. For the last job, it deletes through the end of the jobs block, plus the blank lines directly above the job. Removing the only job also deletes the `jobs:` line.
  - Verify before writing: load the result together with the other config files. The edited file must have exactly the old job names plus or minus NAME. Every other job's digest, description, and `enabled` must be unchanged, and so must every non-job setting. An added job's digest must equal the requested definition's. Any mismatch fails with `config_edit` and writes nothing.
- Writing: a new `.noon.yaml` is created with mode 0600 and a new `noon.yaml` with 0644; existing files keep their mode. Write atomically: temp file in the same directory, fsync, rename.
- Ignore check: inside a Git work tree, if `git check-ignore -q .noon.yaml` says it is not ignored, warn with the line to add. Never edit .gitignore.
- Enabling: if the job has a schedule or a queued run, enable the project without approving other shared jobs, and approve the new job when `--shared`. Mention other shared jobs that still need approval. In the same store transaction, delete any `job_state` row for NAME, so a re-added job never catches up on a stale occurrence.
- Output follows the design transcript: added line with file, privacy, normalized schedule, and next run; enable line; then one `Next:` line (service install when no scheduler is running, otherwise `noon run NAME`).
- `noon remove NAME` (alias `rm`) deletes the job from the file that defines it. A job defined in both files needs `--shared` or `--local` (usage error otherwise). History is kept. Bare `noon remove` lists candidates.
- `noon pause NAME` and `noon resume NAME`: migration 5 adds `pauses(project_id INTEGER NOT NULL REFERENCES projects(id), job TEXT NOT NULL, paused_at TEXT NOT NULL, PRIMARY KEY(project_id, job))`. A paused job is inactive for the planner, which cancels its queued runs. Resume deletes the pause row and the job's `job_state` row in one transaction, so it starts from now with no catch-up, even if the scheduler never saw the pause. Foreground `noon run` still works. Bare forms list candidates. List and bare `noon` show `paused`; list JSON adds `paused`.
- The empty-project guidance from the list task now points to `noon add`.

Non-goals: editing fields of existing jobs, interactive prompts, writing user config or explicit config files, `--force` overwrite, pausing all jobs, editing .gitignore, reformatting files.

Tests:
- internal/config `TestEditJobs`, a table of fixtures: 4-space indentation, blank lines between jobs, head, inline, and trailing comments, a last job whose `run` is a `|` block scalar, CRLF line endings, a file without `jobs`, and an empty file. For each, add leaves every original byte in place (deleting the inserted lines restores the original), and removing the added job returns the original bytes exactly. Every result loads with `config.Load`. Removing an existing middle job and the only job each produce the expected bytes. `jobs: {}` is refused with `config_edit`.
- internal/cli `TestAddJobs`: each action form writes the expected YAML; an invalid schedule leaves files byte-identical, prints the suggestion, and exits 2; a name collision is refused; `--config` is refused with `explicit_config` and nothing written; new files get modes 0600 and 0644; `--shared` approves only the added job while another changed shared job stays `needs_approval`; `--in` queues a run; a scheduled add enables the project.
- internal/cli `TestPauseResume`: a paused job is not dispatched by the serve loop (injected clock) and is listed as paused; resume does not catch up, including when pause and resume both happen while the loop is stopped after an occurrence passed.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^(TestEditJobs|TestAddJobs|TestPauseResume)$' -count=1 -v` exits 0 and prints `--- PASS` for all three, covering every case listed under Tests
- [ ] #2 In one shell: `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && git -C "$noon_ac_project" init -q && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" add tests "every day at 2am" -- go test ./...` exits 0 and prints the normalized schedule `every day at 02:00` with a next run time; repeating the same command exits 1 with `exists`; `env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" add bad "every day" -- true; echo "exit=$?"` prints a suggestion containing `every day at 9am` and `exit=2`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
