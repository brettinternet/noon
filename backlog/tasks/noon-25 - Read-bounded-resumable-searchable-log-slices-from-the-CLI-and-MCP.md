---
id: NOON-25
title: 'Read bounded, resumable, searchable log slices from the CLI and MCP'
status: To Do
assignee: []
created_date: '2026-10-16 17:26'
labels:
  - cli
  - mcp
dependencies:
  - NOON-24
references:
  - internal/app/history.go
  - internal/cli/history.go
  - internal/mcp/server.go
  - ../hum/internal/cli/commands.go
documentation:
  - docs/design.md
  - docs/development.md
  - ../hum/docs/design.md
  - ../hum/docs/coding-agents.md
modified_files:
  - internal/app/**
  - internal/cli/**
  - internal/mcp/**
  - README.md
  - docs/design.md
priority: high
type: feature
ordinal: 2020
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: developers and agents read exactly the part of a run log they need: the last N lines, the lines matching a pattern with context, or the next page after the previous read. Repeated reads never lose or repeat output.

Why: `noon logs` prints the whole file (up to about 2 MiB plus hook output), and MCP `get_run` returns only a byte tail of at most 64 KiB, which can cut mid-line. To find one error in a large log, an agent has to pull the whole thing into context or guess byte counts, and there is no way to resume where it stopped while a run is still writing. The sibling project hum (`../hum`) solved this for agents with bounded reads, a resumable cursor that returns `next`, and `--match` with `--context`. See `../hum/docs/design.md` (section "hum logs") and `../hum/docs/coding-agents.md` (Workflows). Borrow those semantics, adapted to Noon's byte-oriented log files. Do not borrow hum's per-entry storage, `--since`, multi-name reads, or stream filters (see Non-goals).

Read first: docs/design.md#experience, #execution (log capture: first 1 MiB written live, last 1 MiB appended after an omission marker at exit), and #architecture (rules live in internal/app; CLI and MCP only parse and render). Then read NOON-17 and NOON-24, and the existing `GetRun`, `ReadRunLog`, and `FollowLog` in internal/app/history.go.

Log file facts the design relies on: a run log only grows. Bytes are appended live, then the omission marker and tail are appended at exit, then hook output is appended after the hook. So a byte offset into the file is a stable cursor for the life of the retained log.

Scope:
- One shared read operation in internal/app, used by `noon logs` and MCP `get_run`. It takes one snapshot read of the file and applies the query below. It returns the selected text plus cursor metadata. `FollowLog` stays separate. Keep the selection logic a pure function over bytes so it can be unit-tested without a store.
- Query and selection order:
  1. Range: `after` is a byte offset. With `after`, scan `[after, size)`; without it, scan `[0, size)`. A negative `after`, or one greater than `size`, is a usage error.
  2. Active runs: while the run or its hook is active (`activeStoredRun`), exclude a trailing line that has no terminating newline yet. For a terminal run, include the final unterminated line. This keeps pages ending on line boundaries while output is still arriving.
  3. Lines: a line is the bytes up to and including `\n`. If `after` falls mid-line, the scan starts with that partial line.
  4. Match: `match` is a Go RE2 regular expression, case-sensitive (`(?i)` opts out), applied to each line without its trailing newline. `context` (0–50, default 0, requires `match`) adds up to N lines on each side. Context never crosses the scan range. Overlapping or adjacent windows merge in order without duplicates. Output uses `grep -n` style: `LINE:text` for matching lines, `LINE-text` for context lines, and a `--` line between non-adjacent windows. LINE is the 1-based line number counted from the start of the file, even when `after` is set.
  5. Tail: `tail` (1–10000) keeps the last N selected output lines.
  6. Byte limit: `limit_bytes` caps the emitted text. Without `after`, keep the newest whole lines. With `after` (forward paging), keep the earliest whole lines. If a single line exceeds the limit, keep its last bytes (tail mode) or first bytes (forward mode), cut on a UTF-8 rune boundary. Set `truncated: true` whenever the limit dropped or cut anything.
  7. Cursor: `next` is the offset to pass back as `after`. In forward mode, when the limit dropped content, `next` is the file offset just past the last returned byte. In every other case, `next` is the end of the scanned range. `next` is never less than `after`. Passing `next` back never repeats or skips a byte (match mode skips non-matching bytes by design).
- Result fields, added to both CLI `logs --json` and MCP `get_run` results: `log` (text, invalid UTF-8 replaced as today), `next` (int), `size` (file bytes at read time), `truncated` (bool), `matched_lines` (number of lines that matched, or null when there was no `match`). The existing `run` and `schema_version` fields stay. Without query flags, `noon logs --json` keeps returning the full log in `log`.
- CLI `noon logs [NAME|RUN]` flags, named like hum's: `--tail/-n N`, `--after-cursor/-c BYTES`, `--match/-m REGEX`, `--context N`, `--limit-bytes/-b N`. Human output prints the selected text to stdout. When any of these flags is given, also print one stderr line `next: noon logs RUN --after-cursor NEXT`, so stdout stays clean for pipes. With no flags, behavior is unchanged: the full stored bytes, printed unmodified.
- `--follow` accepts `--tail` and `--after-cursor` only, to choose where following starts (`--tail 20 --follow` behaves like `tail -n 20 -f`). After that, the existing follow behavior is unchanged, including the 1 MiB notice. `--follow` with `--match`, `--context`, or `--limit-bytes` is a usage error. So is `--path` (from NOON-24) combined with any selection flag.
- MCP `get_run`: add optional `after`, `tail`, `match`, and `context` arguments with the same meaning. The existing `log_bytes` is the byte limit (default 16384, maximum 65536). With no new arguments, `get_run` still returns a newest tail of at most `log_bytes`, now trimmed to whole lines. Update the tool and argument descriptions (the jsonschema tags) so agents learn to page with `next` and search with `match`.
- Invalid arguments (bad regex, out-of-range numbers, `context` without `match`) are usage errors: exit 2 in the CLI, a `usage` error envelope in MCP.
- Update docs/design.md (the `noon logs` command row and a short contract next to the #execution log bullets) and README.md (Logs and MCP sections).

Non-goals: storing stdout and stderr separately or adding per-line timestamps (logs stay raw combined bytes, readable with `cat` and `rg`), `--since`, multi-run or multi-job reads, live regex following, changing retention or the 1 MiB capture bounds, a new MCP tool (extend `get_run`), and hook/job sections (NOON-28).

Tests (critical regressions only):
- internal/app `TestSelectLog` (new internal/app/history_test.go, table-driven over in-memory bytes):
  - Forward paging with a small limit, over content that includes multi-byte UTF-8 and one line longer than the limit, reproduces the input exactly when pages are concatenated, never repeats a byte, and ends with `next == size`.
  - `match` with `context` merges overlapping and adjacent windows and numbers lines from the start of the file when `after > 0`; context does not cross `after`.
  - Tail mode with a limit keeps the newest whole lines, and an over-long single line keeps its last bytes on a rune boundary with `truncated: true`.
  - For an active run, a trailing unterminated line is excluded and `next` stops before it; for a terminal run it is included.
- internal/cli `TestHistoryAndLogs`: `--tail 2` output. The usage errors in acceptance criterion 5 exit 2.
- internal/cli `TestLogsFollow`: `--tail 1 --follow` started while a job runs prints its latest line and later output, but not earlier lines.
- internal/mcp `TestMCP`: `get_run` with `match` returns the matching window and `matched_lines`. A second call with `after: next` on a terminal run returns an empty `log`. A default `get_run` returns at most 16384 bytes of whole lines.

AC fixture: the same isolated prefix as NOON-24 (`t` is run 1: lines `line-1`..`line-2000`, then `boom`, 18898 bytes in total; `ok` is run 2). If the shell tool rejects inline syntax, run the exact command from a temporary script and note that.

Fixture prefix (`N` below means `env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project"`): `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; `

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^(TestSelectLog|TestHistoryAndLogs|TestLogsFollow|TestMCP)$' -count=1 -v` exits 0 and prints `--- PASS` for each, covering every case listed under Tests
- [ ] #2 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs t --tail 2` prints exactly two lines: `line-2000` and `boom`
- [ ] #3 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs t --match boom --context 1` prints exactly two lines: `2000-line-2000` and `2001:boom`
- [ ] #4 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" --json logs t --after-cursor 0 --limit-bytes 100 | jq -c '{next, truncated, size}'; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" --json logs t --after-cursor 95 --limit-bytes 8 | jq -r .log` prints `{"next":95,"truncated":true,"size":18898}`, then `line-14`
- [ ] #5 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs t --context 2; echo "exit=$?"; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs t --match '('; echo "exit=$?"; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs t --follow --match boom; echo "exit=$?"; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" logs t --path --tail 1; echo "exit=$?"` prints four usage errors, each followed by `exit=2`
- [ ] #6 `task cli:build && noon_ac_home=$(mktemp -d) && noon_ac_project=$(mktemp -d) && printf 'jobs:\n  t:\n    run: "seq 1 2000 | sed s/^/line-/; echo boom; exit 3"\n  ok:\n    run: "echo fine"\n  slow:\n    run: "echo started; sleep 30"\n  hooked:\n    run: "echo job-out; exit 3"\n    on_failure: [sh, -c, "echo hook-out"]\n' > "$noon_ac_project/noon.yaml" && env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run t >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" run ok >/dev/null 2>&1; env -i PATH="$PATH" HOME="$noon_ac_home" bin/noon -C "$noon_ac_project" --json logs t --tail 1 | jq -c '{log, next, size, matched_lines}'` prints `{"log":"boom\n","next":18898,"size":18898,"matched_lines":null}`
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
