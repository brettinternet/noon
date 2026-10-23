---
id: NOON-14
title: Adopt one CLI error and exit-code contract
status: done
type: feature
priority: high
labels: [cli, reviewed]
rank: a02fa0be82fa0be82
refs: [docs/design.md, docs/development.md]
files: [internal/cli/**, scripts/smoke.sh, README.md]
created: 2026-10-16T01:41:00Z
updated: 2026-10-16T14:18:00Z
---

Outcome: every failure renders through one typed error, so all later commands inherit the documented contract: human `noon: MESSAGE` plus an optional `Hint: ...` line on stderr; JSON `{"schema_version":1,"error":{"code":"...","message":"...","hint":"..."}}` as the only stdout; exit 2 for usage errors and 1 for everything else.

Read first: docs/design.md#experience. Current code: internal/cli/root.go (`Run`, `jsonRequested`, `emit`) and internal/cli/root_test.go.

Scope:
- Add one error type in internal/cli carrying `Code`, `Message`, `Hint`, and whether it is a usage error, with small constructors. Omit `hint` from JSON when empty.
- Codes in this task: `usage` (exit 2), `config` (errors returned by config.Load or config.ProjectDir), `error` (anything else). Later tasks add codes; keep them lowercase snake_case.
- Usage errors: unknown commands and flags, unexpected arguments, empty flag or environment values, and flags a command rejects (init's override flags).
- `jsonRequested` must derive which flags take values from the command tree (every non-bool flag, including aliases), not a hard-coded list, so new flags cannot be misread as `--json`. Keep today's guarantee that failures before flag parsing still produce JSON.
- Update scripts/smoke.sh so it checks the new error object and the exit code of a usage error.
- README: one short paragraph stating the JSON envelope and exit codes.

Non-goals: new commands or flags, colors, changing success output, `Next:` helpers.

Notes:
- urfave/cli v3 routes flag errors through `OnUsageError`; unknown subcommands and extra arguments reach actions through `noArgs`. Both must become usage errors.
- Keep `cli.Run` the only place that renders errors.

Modified-file contract: the modified_files list plus this task's Backlog record. Justify any other path in Implementation Notes before changing it.

## Acceptance criteria

- [x] `task cli:test -- -run '^TestJSONFailuresAreSingleObjects$' -count=1 -v` exits 0 and prints `--- PASS: TestJSONFailuresAreSingleObjects`; the test asserts every case returns exactly one JSON object with non-empty `error.code` and `error.message`, exit 2 for the unknown-flag, unexpected-argument, and empty-value cases, and exit 1 with code `config` for the missing explicit config file
- [x] `task cli:test -- -run '^TestValueFlagsAreNotMistakenForJSON$' -count=1 -v` exits 0: an invocation whose string flag value is literally `--json` (for example `--timezone --json`) is not treated as a JSON request
- [x] `task cli:build && bin/noon --bogus; echo "exit=$?"` prints a `noon: ` line on stderr and `exit=2`; `bin/noon --json --config missing.yaml config check; echo "exit=$?"` prints one JSON object whose `error.code` is `config`, then `exit=1`
- [x] `task smoke` exits 0

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Plan

1. Inspect CLI parsing and error rendering. 2. Add typed errors and command-derived JSON detection; update regression tests, smoke, and README. 3. Run acceptance checks and CI, review, commit, merge, and clean up.

## Notes

AC#1: task cli:test -- -run '^TestJSONFailuresAreSingleObjects$' -count=1 -v -> exit 0, PASS; single-object envelope, usage/config exit codes, empty environment values verified.
AC#2: task cli:test -- -run '^TestValueFlagsAreNotMistakenForJSON$' -count=1 -v -> exit 0, PASS for all current value flags including -C.
AC#3: task cli:build && bin/noon --bogus; echo "exit=$?" -> noon: diagnostic, exit=2. bin/noon --json --config missing.yaml config check; echo "exit=$?" -> one config error object, exit=1.
AC#4: task smoke -> exit 0, config and usage error objects and exit codes verified.
Pre-commit task ci passed (vet, Staticcheck, tests, race, secrets, vulnerabilities, smoke). LSP root.go clean. One scoped diff review found no concrete defects. All changes within contract; no tests removed, skipped, or weakened. README documents implemented behavior; existing design Experience contract remains consistent.

Delivered commit 78faa977455da3ea336dda545be3b37df5c3cc23, fast-forward merged into main. task ci passed on that final implementation commit. Worktrunk removed the verified session-owned worktree and branch; its Herdr workspace was removed. No blockers or remaining implementation steps; no push requested.

Review (post-merge): implementation matches the contract. Probed usage/config/JSON paths against the built binary: unknown flags, extra args, bad/empty flag values, and value flags whose value is --json all behave as specified. Low-severity gap: `noon help bogus` is urfave's own exit error, rendered as code `error` with exit 1 instead of `usage`/2. ACs 1-2 rerun and pass.

## Summary

Implemented typed CLI errors, usage exit 2 versus operational exit 1, structured JSON errors with optional hints, and command-tree-derived value-flag detection. All four acceptance checks and full CI passed; committed and merged into main, isolated checkout cleaned up.
