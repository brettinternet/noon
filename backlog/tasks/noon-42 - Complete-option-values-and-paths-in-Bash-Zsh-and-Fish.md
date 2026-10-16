---
id: NOON-42
title: Complete option values and paths in Bash Zsh and Fish
status: To Do
assignee: []
created_date: '2026-10-16 23:22'
labels:
  - cli-ux
  - devex
dependencies: []
modified_files:
  - internal/cli/shell.go
  - internal/cli/shell/**
  - internal/cli/shell_test.go
  - integration/shell_test.go
  - docs/shell.md
priority: medium
type: enhancement
ordinal: 9400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: tab completion helps finish the argument users are currently entering, not just command and flag names.

Evidence: completeShell in internal/cli/shell.go returns immediately when expecting any flag value. Built-binary completion for logs --status, logs --section and shell init with an empty current token returns no candidates. Bash registers completion without a file fallback, Zsh uses only compadd, and Fish disables file completion, so path-valued flags are also underserved.

Scope: complete supported shell names, run statuses (including comma-separated/repeated filters), log sections and configured agent names in the correct positions; use shell-native filesystem completion for project directories, config/prompt files and crontab input paths. Handle separate values and --flag=value, existing aliases, quoting and -- boundaries. Keep static candidates usable with broken config. Continue reading config only; do not introduce state/run-ID/history completion.

Non-goals: starting jobs while completing, database creation/migration, startup-file edits, external completion frameworks, network calls or new runtime dependencies.
Modified-file contract: internal/cli/shell.go, internal/cli/shell/**, internal/cli/shell_test.go, integration/shell_test.go, docs/shell.md; explain expansion.
Dependencies: none; NOON-30 is completed. Work from the current flag tree so the separate short-flag enhancement is not a prerequisite.

Acceptance commands describe the intended implementation, not current behavior. Isolate HOME, XDG_CONFIG_HOME, XDG_STATE_HOME and NOON_* settings in temporary fixtures; never touch real jobs or services. Implement before adding tests, extend existing public-interface tests only for parsing, safety, or realistic regressions, and record command/result evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run '^TestShellCompletion$' -count=1 -v` exits 0. Extend existing parser/safety coverage for value positions, equals syntax, comma-separated statuses, quoted paths and --; never emit job names as unrelated values or run a command.
- [ ] #2 `task shell:smoke` exits 0 in real Bash, Zsh and Fish with fixtures for shell/status/section/agent values and directory/file paths containing spaces and metacharacters. Completion preserves argument boundaries and shell status, writes no config/state, and opens no service/listener. Missing shells remain explicit failures, not skips.
- [ ] #3 `task cli:build` exits 0; direct `bin/noon shell init '' --generate-shell-completion`, `bin/noon logs --status '' --generate-shell-completion` and `bin/noon logs --section '' --generate-shell-completion` under isolated HOME return the relevant candidates. docs/shell.md accurately describes value/path completion and safety limits.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
