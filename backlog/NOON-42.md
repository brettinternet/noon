---
id: NOON-42
title: Complete option values and paths in Bash Zsh and Fish
status: done
type: enhancement
priority: medium
labels: [cli-ux, devex, reviewed]
rank: a7d05f417d05f4154
files: [internal/cli/shell.go, internal/cli/shell/**, internal/cli/shell_test.go, integration/shell_test.go, docs/shell.md]
created: 2026-10-16T23:22:00Z
updated: 2026-10-17T02:36:00Z
---

Outcome: tab completion helps finish the argument users are currently entering, not just command and flag names.

Evidence: completeShell in internal/cli/shell.go returns immediately when expecting any flag value. Built-binary completion for logs --status, logs --section and shell init with an empty current token returns no candidates. Bash registers completion without a file fallback, Zsh uses only compadd, and Fish disables file completion, so path-valued flags are also underserved.

Scope: complete supported shell names, run statuses (including comma-separated/repeated filters), log sections and configured agent names in the correct positions; use shell-native filesystem completion for project directories, config/prompt files and crontab input paths. Handle separate values and --flag=value, existing aliases, quoting and -- boundaries. Keep static candidates usable with broken config. Continue reading config only; do not introduce state/run-ID/history completion.

Non-goals: starting jobs while completing, database creation/migration, startup-file edits, external completion frameworks, network calls or new runtime dependencies.
Modified-file contract: internal/cli/shell.go, internal/cli/shell/**, internal/cli/shell_test.go, integration/shell_test.go, docs/shell.md; explain expansion.
Dependencies: none; NOON-30 is completed. Work from the current flag tree so the separate short-flag enhancement is not a prerequisite.

Acceptance commands describe the intended implementation, not current behavior. Isolate HOME, XDG_CONFIG_HOME, XDG_STATE_HOME and NOON_* settings in temporary fixtures; never touch real jobs or services. Implement before adding tests, extend existing public-interface tests only for parsing, safety, or realistic regressions, and record command/result evidence.

## Acceptance criteria

- [x] `task cli:test -- -run '^TestShellCompletion$' -count=1 -v` exits 0. Extend existing parser/safety coverage for value positions, equals syntax, comma-separated statuses, quoted paths and --; never emit job names as unrelated values or run a command.
- [x] `task shell:smoke` exits 0 in real Bash, Zsh and Fish with fixtures for shell/status/section/agent values and directory/file paths containing spaces and metacharacters. Completion preserves argument boundaries and shell status, writes no config/state, and opens no service/listener. Missing shells remain explicit failures, not skips.
- [x] `task cli:build` exits 0; direct `bin/noon shell init '' --generate-shell-completion`, `bin/noon logs --status '' --generate-shell-completion` and `bin/noon logs --section '' --generate-shell-completion` under isolated HOME return the relevant candidates. docs/shell.md accurately describes value/path completion and safety limits.

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Notes

Implementation is in owned worktree .worktrees/feat-noon-42-completion on feat/noon-42-completion. Initial focused completion, three-shell smoke, build and full task ci passed. Single independent review identified Bash empty equals path completion and Zsh greedy equals-prefix stripping; both corrected, targeted native-shell regression validation in progress. No modified-file contract expansion. Next: finish regression checks, commit, run final-commit CI, merge locally and remove owned worktree.

AC#1: task cli:test -- -run '^TestShellCompletion$' -count=1 -v -> exit 0; value positions, aliases, equals, comma statuses, quoted config paths, boundaries and read-only behavior covered.
AC#2: task shell:smoke -> exit 0 in Bash, Zsh and Fish, including live zpty insertion checks and paths with spaces, metacharacters and equals. Independent review found and corrections fixed empty Bash equals-prefix and greedy Zsh equals-prefix defects; focused regression checks pass.
AC#3: task cli:build -> exit 0. Direct bin/noon shell init '' --generate-shell-completion, bin/noon logs --status '' --generate-shell-completion and bin/noon logs --section '' --generate-shell-completion with env -i and isolated HOME/XDG_CONFIG_HOME/XDG_STATE_HOME -> shells bash/zsh/fish, ten statuses, sections all/job/hook. docs/shell.md updated with implemented value/path completion and safety limits.
Delivery: implementation commit 8e1057f; integrated current main in 86d9377, task ci and task shell:smoke passed on that final code commit, then fast-forwarded main. task check:staged passed. No tests removed/skipped/weakened; obsolete no-value expectations replaced by positive assertions. Diff within allowed files; no design-contract change. Go LSP diagnostics clean. Worktrunk removed owned worktree and branch; matching Herdr workspace wF8 verified absent. Unrelated primary changes and pre-existing feat/tui-design worktree preserved. No push. No blocker or remaining resumable step. Executor configured Fish through global Mise to satisfy required installed-shell smoke prerequisite.

Review: confirmed in real interactive Bash (tmux) that global complete -o filenames turned job-name candidates into directory paths (noon run test<TAB> -> tests/ when ./tests exists). Fixed by enabling filename handling only for path values via compopt (Bash 3.2, lacking compopt, keeps the previous registration). Replaced the duplicated run-status list with app.RunStatuses. task shell:smoke and task test pass.

## Summary

Completed read-only value and native filesystem completion across Bash, Zsh and Fish; reviewed, tested, committed and merged locally. Owned worktree/branch/workspace removed.
