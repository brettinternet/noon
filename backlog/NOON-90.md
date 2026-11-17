---
id: NOON-90
title: Preserve stdout and stderr order in run logs
status: todo
type: bug
priority: medium
labels: [runner, delight]
refs: [docs/design.md]
files:
  - internal/runner/runner.go
  - internal/runner/runner_test.go
  - internal/cli/run.go
  - docs/design.md
created: 2026-11-17T18:40:23Z
updated: 2026-11-17T18:40:46Z
---

Why: the run log is how people and agents learn what a job did, and docs/design.md promises "The log interleaves stdout and stderr." It doesn't: the runner gives the child two pipes and copies each in its own goroutine, so a script that alternates `echo out N` and `echo err N >&2` shows its `err` lines first, both in the terminal of a foreground `noon run` and in `noon logs`. Error messages then appear far from the output that caused them, which is exactly the context a failure read needs.

Outcome:

```console
$ noon logs build
compiling a
error: a.go:3: undefined: x
compiling b
```

Settled contract:
- `runner.Spec`: when `Stdout` and `Stderr` are the same writer, including both nil (the scheduler), the child gets one pipe as fd 1 and fd 2 and one copy loop, so the log and the destination keep the child's write order. Hooks, which pass `capture` for both, get this automatically.
- Foreground `noon run` passes one writer for both streams when its stdout and stderr are the same open file (both `*os.File` and `os.SameFile` on their `Stat`): a shared terminal or `>file 2>&1`. `--json` already sends both to stderr.
- When the caller's stdout and stderr are different destinations (`noon run job 2>err.txt`), keep two pipes so each stream still reaches its own destination; cross-stream order in the log is then best-effort. docs/design.md states this rule instead of the unconditional "interleaves".
- The child sees pipes, never a pty. Log size limits, the omission marker, `--follow`, and line receipt times (`--timestamps`) are unchanged.

Scope: internal/runner/runner.go pipe setup, the foreground writer choice in internal/cli/run.go, docs/design.md run-log rule.

Tests (critical only):
- Regression: with nil writers and with one shared writer, a child alternating stdout and stderr lines yields exactly that order in `Log`, repeated enough iterations to fail reliably on the two-pipe implementation.
- Data integrity: with distinct writers, each receives only its own stream. Extend TestRunner.

Non-goals: pty allocation; tagging log lines with their stream; merging streams the caller asked to keep separate; changing hook or scheduler log layout.

Dependencies: none.

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [ ] After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `noon_ac_home=$(mktemp -d)` and a temp Git repo whose noon.yaml defines manual job `order` with run ["sh", "-c", "for i in 1 2 3 4 5; do echo out $i; echo err $i >&2; done"]: across 20 repetitions, `bin/noon run order >"$noon_ac_home/both" 2>&1` exits 0 and the lines matching `^(out|err) ` in `both` and in `bin/noon logs order` are exactly `out 1`, `err 1`, `out 2`, `err 2`, … `err 5` in that order.
- [ ] In the same environment, `bin/noon run order >"$noon_ac_home/out" 2>"$noon_ac_home/err"` exits 0, `out` contains exactly the five `out` lines, and `err` contains the five `err` lines and no `out` line.
- [ ] `task cli:test PKG=./internal/runner -- -count=1` exits 0 and covers: with nil Stdout/Stderr and with one shared writer, alternating child stdout/stderr lines keep their order in Log; with distinct writers, each receives only its own stream.

## Definition of done

- [ ] task ci passes on the final commit
- [ ] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] No test was deleted, skipped, or weakened
- [ ] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
