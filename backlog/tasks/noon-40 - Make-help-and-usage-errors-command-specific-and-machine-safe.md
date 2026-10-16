---
id: NOON-40
title: Make help and usage errors command-specific and machine-safe
status: To Do
assignee: []
created_date: '2026-10-16 23:22'
labels:
  - cli-ux
  - devex
dependencies: []
modified_files:
  - internal/cli/**
  - README.md
  - docs/cli.md
  - docs/design.md
  - docs/shell.md
priority: high
type: bug
ordinal: 7400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users can recover from a typo with the relevant command help, and scripts never mistake ignored input or human text for success.

Verified evidence: import typo exits 0 and lists importers because importers ignores positional args. run --detah exits 2 but points only to noon --help, where detach is not explained. remove --help has blank descriptions for shared/local. shell --json exits 0 with plain help, violating the documented JSON action contract. docs/cli.md says serve accepts only state-dir even though serve --listen is implemented.

Scope: reject unknown/extra positionals consistently at command groups, retain successful bare-group guidance, direct usage errors to the full command path, and suggest unambiguous nearby command/flag names without autocorrection. Make shell --json return a versioned structured guidance object (explicit --help/version/completion remain documented text exceptions). Improve root help's dashboard default and everyday workflow examples, leaf examples/argument/value placeholders, remove scope descriptions, and precise stream/JSON and serve flag documentation.

Non-goals: changing exit-code meanings, auto-running corrected commands, exposing secrets in examples, custom help frameworks or new commands.
Modified-file contract: internal/cli/**, README.md, docs/cli.md, docs/design.md, docs/shell.md; explain expansion.
Dependencies: none; follow-up to the completed NOON-14 contract, not a replacement.

Acceptance commands describe the intended implementation, not current behavior. Isolate HOME, XDG_CONFIG_HOME, XDG_STATE_HOME and NOON_* settings in temporary fixtures; never touch real jobs or services. Implement before adding tests, extend existing public-interface tests only for parsing, safety, or realistic regressions, and record command/result evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:build` exits 0. Isolated `bin/noon import typo` and `bin/noon config typo` exit 2 with actionable group-specific guidance; `bin/noon run --detah` points to `noon run --help` and offers --detach without executing anything. Bare import/config retain their successful defaults.
- [ ] #2 `task cli:test -- -run '^(TestJSONFailuresAreSingleObjects|TestValueFlagsAreNotMistakenForJSON|TestInspectionDoesNotWriteOrExecute)$' -count=1 -v` exits 0. Extend existing parsing/serialization regressions for rejected group input and shell --json: one schema_version=1 object, no human text contamination, correct exit code, and no side effects. Explicit --help remains text.
- [ ] #3 Inspect `bin/noon --help`, `bin/noon add --help`, `bin/noon remove --help`, `bin/noon logs --help`, `bin/noon serve --help` and corresponding docs after `task cli:build`: safe defaults, common examples, flag values/scope, and output-mode exceptions are accurate. `task test` and `task smoke` exit 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
