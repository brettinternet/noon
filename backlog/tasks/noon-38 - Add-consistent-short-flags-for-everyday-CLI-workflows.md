---
id: NOON-38
title: Add consistent short flags for everyday CLI workflows
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
  - docs/shell.md
priority: high
type: enhancement
ordinal: 5400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: common interactive commands need fewer keystrokes without sacrificing predictable parsing or machine output.

Evidence: the built binary rejects logs -f and history -n 5 with exit 2. internal/cli/root.go defines no aliases for follow, limit, detach, status, or add's common agent flags; only project and four log selectors have short forms.

Scope: add -f/--follow, -n/--limit on history, -s/--status on history and logs, -d/--detach on run, -a/--agent, -p/--prompt and -t/--timeout on add, and -j/--json globally. Keep existing -C, -n/-c/-m/-b on logs, -h and -v unchanged. Audit remaining flags and document deliberate long-only choices where aliases would collide or be surprising; do not force one-letter aliases onto every option. Help, completion and examples must reflect the same mapping. Audit raw-argv consumers jsonRequested, addArgv and completeShell, not only flag declarations.

Non-goals: command renames, changing queue/approval semantics, bundling short flags unless already supported safely, or taking -c from logs for global config.
Modified-file contract: internal/cli/**, README.md, docs/cli.md, docs/shell.md; explain expansion in task notes.
Dependencies: none; existing parser and completion facilities are implemented.

Acceptance commands describe the intended implementation, not current behavior. Isolate HOME, XDG_CONFIG_HOME, XDG_STATE_HOME and NOON_* settings in temporary fixtures; never touch real jobs or services. Implement before adding tests, extend existing public-interface tests only for parsing, safety, or realistic regressions, and record command/result evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:build` exits 0. In an isolated fixture, `bin/noon logs -h`, `bin/noon history -h`, `bin/noon run -h`, `bin/noon add -h` and `bin/noon -h` show the specified short forms and clear value placeholders; inspect the remaining flag inventory and record justified long-only choices.
- [ ] #2 `task cli:test -- -run '^(TestJSONFailuresAreSingleObjects|TestValueFlagsAreNotMistakenForJSON|TestAddJobs|TestShellCompletion)$' -count=1 -v` exits 0. Extend these existing tests only where needed to prove short-form parsing does not reinterpret argv after -- or consume a value equal to -j/--json, and -j emits one JSON error object even for pre-action parse failures.
- [ ] #3 `task test` and `task smoke` exit 0. Directly exercise equivalent long/short history and log selections, an isolated completed log with -f, add using -a/-p/-t, and -d queueing in an isolated enabled fixture without starting a scheduler. Results and safety semantics match their long forms; completion exposes the aliases.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
