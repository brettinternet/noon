---
id: NOON-28
title: Separate hook output from job output in log reads
status: To Do
assignee: []
created_date: '2026-10-16 17:26'
labels:
  - cli
  - store
  - mcp
dependencies:
  - NOON-27
references:
  - internal/store/store.go
  - internal/app/history.go
documentation:
  - docs/design.md
  - docs/development.md
  - docs/hooks.md
modified_files:
  - internal/store/**
  - internal/app/**
  - internal/cli/**
  - internal/mcp/**
  - README.md
  - docs/design.md
  - docs/hooks.md
priority: low
type: feature
ordinal: 2050
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a reader can ask for only the job's output or only the hook's output, and agents diagnosing a failure are not misled by `on_failure` hook output at the end of the log.

Why: NOON-22 appends a hook's output (up to 64 KiB) to the run log after an in-band marker such as `[noon: on_failure hook exited 0]`. Tail reads, including the default 16 KiB MCP `get_run` tail, therefore show hook output first, and a chatty hook can push the job's real error out of the window entirely. Telling the sections apart by searching for the marker text is unreliable, because job output can contain the same text. Recording the boundary offset is exact and cheap.

Read first: docs/hooks.md, docs/design.md#integrations and #state, NOON-22, and NOON-25. In code, read `store.CompleteHook` (it appends an optional `\n` separator, then the marker, then the output, all after the run is terminal) and the `has*` capability flags plus append-only `migrations` in internal/store/store.go.

Scope:
- Migration: add `runs.hook_log_offset INTEGER` (nullable), following the existing append-only migration and capability-flag pattern so older databases still read. `CompleteHook` sets it to the file offset where the `[noon:` hook marker begins. The optional separator newline belongs to the job section. Leave it null when no hook output was appended.
- Run JSON: `log.hook_offset` (int or null).
- Log reads (the NOON-25 operation): add `section`, one of `all` (default), `job`, or `hook`.
  - `job` limits the scan range to `[0, hook_offset)`, or the whole file when `hook_offset` is null.
  - `hook` limits it to `[hook_offset, size)`, or an empty range when null.
  - Cursors stay absolute file offsets. The range is the intersection of `[after, size)` and the section, and `next` is never less than `after`.
  CLI: `noon logs --section job|hook|all`. MCP `get_run`: `section`. An invalid value, or `--section` with `--follow`, is a usage error.
- While a hook is running, its output has not been appended yet, so `job` covers everything written so far.
- Update docs/hooks.md, docs/design.md, and README.md. In the `get_run` argument description, tell MCP agents to use `section: "job"` when diagnosing a failure.

Non-goals: separate files for hook output, storing stdout and stderr separately, recording omission-marker offsets, changing hook capture limits, and changing hook stdin or `NOON_LOG_FILE`.

Tests (critical regressions only):
- internal/cli `TestHooks`: `hook_offset` equals the job output's byte length, including when the job output lacks a trailing newline (the separator stays in the job section, so `--section hook` starts with `[noon:`). `--section job --tail 1` returns the job's last line. A run without hook output has a null `hook_offset`, and `--section hook` returns an empty log with exit 0.
- internal/store `TestStore`: a database at the previous schema version still reads runs, and reports `hook_offset` as null.
- internal/app `TestSelectLog`: section ranges intersect correctly with `after`.
- internal/mcp `TestMCP`: `get_run` with `section: "job"` excludes hook output.

AC fixture: the same isolated prefix as NOON-24, whose config also defines `hooked` (prints `job-out`, exits 3, `on_failure` prints `hook-out`). `t` is run 1 and `ok` is run 2, so `hooked` becomes run 3. If the shell tool rejects inline syntax, run the exact command from a temporary script and note that.

Fixture prefix (`N` below means `env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project"`): `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; `

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^(TestHooks|TestStore|TestSelectLog|TestMCP)$' -count=1 -v` exits 0 and prints `--- PASS` for each, covering every case listed under Tests
- [ ] #2 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run hooked >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs hooked --section job; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs hooked --section hook; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" --json logs hooked | jq .run.log.hook_offset` prints `job-out`, then `[noon: on_failure hook exited 0]` and `hook-out`, then `8`
- [ ] #3 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run hooked >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs hooked --section nope; echo "exit=$?"; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs hooked --section job --follow; echo "exit=$?"` prints two usage errors, each followed by `exit=2`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
