---
id: NOON-38
title: Add consistent short flags for everyday CLI workflows
status: done
type: enhancement
priority: high
labels: [cli-ux, devex, reviewed]
rank: a6e23b88ee23b88ca
files: [internal/cli/**, README.md, docs/cli.md, docs/shell.md]
created: 2026-10-16T23:22:00Z
updated: 2026-10-17T02:03:00Z
---

Outcome: common interactive commands need fewer keystrokes without sacrificing predictable parsing or machine output.

Evidence: the built binary rejects logs -f and history -n 5 with exit 2. internal/cli/root.go defines no aliases for follow, limit, detach, status, or add's common agent flags; only project and four log selectors have short forms.

Scope: add -f/--follow, -n/--limit on history, -s/--status on history and logs, -d/--detach on run, -a/--agent, -p/--prompt and -t/--timeout on add, and -j/--json globally. Keep existing -C, -n/-c/-m/-b on logs, -h and -v unchanged. Audit remaining flags and document deliberate long-only choices where aliases would collide or be surprising; do not force one-letter aliases onto every option. Help, completion and examples must reflect the same mapping. Audit raw-argv consumers jsonRequested, addArgv and completeShell, not only flag declarations.

Non-goals: command renames, changing queue/approval semantics, bundling short flags unless already supported safely, or taking -c from logs for global config.
Modified-file contract: internal/cli/**, README.md, docs/cli.md, docs/shell.md; explain expansion in task notes.
Dependencies: none; existing parser and completion facilities are implemented.

Acceptance commands describe the intended implementation, not current behavior. Isolate HOME, XDG_CONFIG_HOME, XDG_STATE_HOME and NOON_* settings in temporary fixtures; never touch real jobs or services. Implement before adding tests, extend existing public-interface tests only for parsing, safety, or realistic regressions, and record command/result evidence.

## Acceptance criteria

- [x] `task cli:build` exits 0. In an isolated fixture, `bin/noon logs -h`, `bin/noon history -h`, `bin/noon run -h`, `bin/noon add -h` and `bin/noon -h` show the specified short forms and clear value placeholders; inspect the remaining flag inventory and record justified long-only choices.
- [x] `task cli:test -- -run '^(TestJSONFailuresAreSingleObjects|TestValueFlagsAreNotMistakenForJSON|TestAddJobs|TestShellCompletion)$' -count=1 -v` exits 0. Extend these existing tests only where needed to prove short-form parsing does not reinterpret argv after -- or consume a value equal to -j/--json, and -j emits one JSON error object even for pre-action parse failures.
- [x] `task test` and `task smoke` exit 0. Directly exercise equivalent long/short history and log selections, an isolated completed log with -f, add using -a/-p/-t, and -d queueing in an isolated enabled fixture without starting a scheduler. Results and safety semantics match their long forms; completion exposes the aliases.

## Definition of done

- [x] task ci passes on the final commit
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [x] No test was deleted, skipped, or weakened
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled

## Plan

1. Audit flag declarations, raw argv parsing, completion, and existing tests. 2. Add requested aliases and document intentional long-only flags; extend critical parsing regressions. 3. Run all acceptance checks and task ci, review scoped diff, commit, merge, record delivery, and clean up owned worktree.

## Notes

Implemented requested aliases with explicit placeholders; addArgv and completeShell already consume alias names from declarations. jsonRequested now scopes value-taking flags to the selected command path, preserving global values after subcommands. Independent reviewer found config -p --json/-j losing JSON error output; fixed and covered alongside nested/global-value cases. No other validated findings; no tests deleted, skipped, or weakened. Source/docs stay within contract; backlog metadata is required workflow bookkeeping. No design contract changed.
AC#1: task cli:build -> exit 0. Isolated env -i PATH="$PATH" sh /var/folders/jp/1mwx72h172955139pth4h8800000gn/T/noon-short-flags.3YoCtQBwp7/acceptance.sh -> exit 0; inspected logs/history/run/add/root -h showing aliases and N/STATUS/TEXT/NAME/DURATION placeholders. docs/cli.md records full flag inventory: config/timezone/state-dir and log context/section/path stay long to avoid collisions; timing, payload, action, scope, and service/shell side-effect flags remain explicit long forms.
AC#2: task cli:test -- -run "^(TestJSONFailuresAreSingleObjects|TestValueFlagsAreNotMistakenForJSON|TestAddJobs|TestShellCompletion)$" -count=1 -v -> exit 0 after review correction. Existing tests extended for literal argv after --, -j/--json values, pre-action single-object errors, command-scoped invalid flags, and alias completion.
AC#3: task test and task smoke -> exit 0, repeated through task ci on final implementation commit 97c8e32. Isolated acceptance.sh compared long/short history and logs JSON byte-for-byte, completed follow output byte-for-byte, added equivalent agent prompt/timeout jobs without running agents, and verified -d/--detach deduplicated to one queued run while scheduler stayed offline. Completion exposed every requested alias.
Validation: task check:staged passed before implementation commit; task ci passed on 97c8e32 (security, formatting, vet, Staticcheck, tests, race, smoke). LSP root.go clean. Optional task shell:smoke passed Bash and Zsh; Fish could not execute because installed mise package has no working fish binary (shim fails under isolated HOME); no shell scripts changed and public completion tests pass. No required acceptance check is blocked. Next: merge verified implementation into main, finalize task record, clean owned worktree, release claim.

Delivery complete: implementation 97c8e323d7b5568f07639fc7aef41481f1f4b123 fast-forward merged into main after rebasing onto concurrent release work. Verified-owned feat/noon-short-flags worktree and branch removed with Worktrunk; its exact Herdr workspace wET disappeared. No task-owned checkout retained; unrelated release/TUI worktrees preserved. No blocker or resumable implementation step remains.

Review: probed built binary (-j/-n/-s/-f/-d/-a/-p/-t, -s=/-n= forms, value flags consuming -j, -j before/after subcommands, alias subcommands); jsonRequested scoping and targeted tests pass. No defects found.

## Summary

Added everyday short flags and consistent help/docs/completion. Preserved literal argv and JSON-like values; corrected command-scoped JSON error detection found by independent review. All ACs and task ci passed on 97c8e32, merged into main; owned worktree/branch/workspace cleaned. Nothing pushed.
