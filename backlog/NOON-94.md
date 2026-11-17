---
id: NOON-94
title: Check job commands against the scheduler's PATH in add and doctor
status: done
type: feature
priority: medium
labels: [cli, ops, delight]
refs: [docs/service.md, docs/cli.md]
files:
  - internal/service/service.go
  - internal/service/service_test.go
  - internal/app/doctor.go
  - internal/cli/manage.go
  - internal/cli/manage_test.go
  - internal/cli/dashboard_test.go
  - docs/service.md
  - docs/cli.md
created: 2026-11-17T18:40:24Z
updated: 2026-11-17T19:06:27Z
---

Why: "works in my shell, fails under the scheduler" is the classic cron surprise, and noon reproduces it. The service unit pins the PATH captured at `noon service install` (plist `EnvironmentVariables`, systemd `Environment=`), so a tool installed later or only on an interactive PATH (mise, nvm, Homebrew in a new prefix) is not found when the job runs unattended; the first sign is a failed run at 2am. `noon doctor` checks each job's command against the current shell's PATH, so it passes in exactly the case that fails in the service. And `noon add typo "every 1h" -- gti status` saves a misspelled command silently.

Outcome:

```console
$ noon add sync "every 1h" -- gti pull
Added sync to … · every 1h
Warning: gti not found on the scheduler's PATH. Suggested action: fix the command, or set PATH in the job's env.
$ noon doctor
✗ job deploy command: terraform not found on the scheduler's PATH; re-run noon service install from a shell where it resolves, or set PATH in the job's env
```

Settled contract:
- `service.InstalledPath(config) (string, bool, error)` returns the PATH from the installed unit, parsed from the same format `BuildUnit` writes; `false` when no unit is installed or the platform is unsupported. The `identity()` drift check keeps ignoring PATH.
- `noon doctor` resolves each job's executable (argv form and agent command) with `runner.ResolveExecutable` against the job environment built as at run time, with the installed unit's PATH replacing the inherited PATH when a unit is installed (job `env`/`env_file` PATH still wins). Without an installed unit, behavior is unchanged. A miss fails the `job NAME command` check with the hint above.
- `noon add` resolves the new job's executable the same way after writing it. A miss prints one `Warning: ... Suggested action: ...` line on stderr (wording as in internal/cli/service.go; "PATH" instead of "the scheduler's PATH" when no unit is installed) and adds `warnings: [string]` to `--json` output. The job is still written and the exit code stays 0, because the command may be installed later or provided by an env file.
- Shell-string jobs are checked only for the shell, as today; the script body is never parsed.
- docs/service.md job-environment section and docs/cli.md (add JSON) describe this.

Scope: internal/service unit PATH parsing, internal/app/doctor.go resolution environment, the add command's post-write check, docs.

Tests (critical only):
- Parsing: `InstalledPath` round-trips the PATH `BuildUnit` writes for launchd and systemd, including characters that need quoting. Extend TestServiceUnits.
- Doctor: with a unit PATH lacking a command that the current PATH resolves, the job command check fails; a job `env` PATH that contains it passes. Extend TestDoctor.

Non-goals: rewriting or reinstalling the unit automatically; parsing shell strings; refusing `add` for unresolved commands; checking prompt or env files at add time (doctor already does).

Dependencies: none.

Modified-file contract: the files list plus this task record. Justify other paths in Notes before changing them.

## Acceptance criteria

- [x] After `task cli:build`, with HOME, XDG_CONFIG_HOME, XDG_STATE_HOME, and NOON_STATE_DIR under one `noon_ac_home=$(mktemp -d)` and a temp Git repo with `bin/noon init`: `bin/noon add typo "every 1h" -- gti status` exits 0, writes job `typo`, and prints exactly one stderr line starting `Warning:` that names `gti`; `bin/noon add typo2 "every 1h" --json -- gti status` exits 0, prints one JSON object on stdout with a one-entry `warnings` array; `bin/noon add fine "every 1h" -- true` prints no `Warning:` line and its JSON has no `warnings`.
  Evidence: AC#1 in Notes: /tmp/noon94-ac/ac1.sh; typo exit 0 + 1 Warning line naming gti; typo2 JSON warnings len 1; fine no warnings.
- [x] `task cli:test PKG=./internal/service -- -run TestServiceUnits -count=1` exits 0 and covers `InstalledPath` round-tripping the PATH written by `BuildUnit` for launchd and systemd, including a PATH that needs quoting.
  Evidence: AC#2 in Notes: TestServiceUnits ok.
- [x] `task cli:test PKG=./internal/cli -- -run TestDoctor -count=1` exits 0 and covers: with an installed unit whose PATH lacks a command the current PATH resolves, the `job NAME command` check fails with the scheduler-PATH hint; a job `env` PATH containing the command passes; without an installed unit, results are unchanged.
  Evidence: AC#3 in Notes: TestDoctor ok incl. scheduler PATH subtest.

## Definition of done

- [x] task ci passes on the final commit
  Evidence: task ci exit 0 on c7b1fcd.
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
  Evidence: Notes list AC#1-3 with commands and results.
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
  Evidence: Diff limited to contract files (manage_test.go untouched).
- [x] No test was deleted, skipped, or weakened
  Evidence: Only test additions in service_test.go and dashboard_test.go.
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
  Evidence: docs/cli.md Command checks and docs/service.md Job environment describe implemented behavior; design.md unaffected.

## Notes

AC#1: /tmp/noon94-ac/ac1.sh after task cli:build (HOME/XDG_CONFIG_HOME/XDG_STATE_HOME/NOON_STATE_DIR under mktemp -d; git init; noon init run with NOON_STATE_DIR unset because init rejects it, pre-existing). add typo -- gti status: exit 0, typo written to .git/noon.yaml, 1 stderr line 'Warning: gti not found on PATH. Suggested action: fix the command, or set PATH in the job's env.'; add typo2 --json: exit 0, one JSON object, warnings has 1 entry; add fine -- true: no Warning line; fine2 --json has no warnings key.

AC#2: task cli:test PKG=./internal/service -- -run TestServiceUnits -count=1 -> ok. Covers unitPath round-trip for launchd and systemd with PATH containing quotes, backslash, %, <&>, ', tab, $HOME; InstalledPath false without a unit and round-trips a written unit on the current platform.

AC#3: task cli:test PKG=./internal/cli -- -run TestDoctor -count=1 -> ok. Subtest 'scheduler PATH': no unit -> job t/pinned command ok (unchanged); unit PATH /usr/bin:/bin lacks tool on the shell PATH -> job t command fail 'tool not found on the scheduler's PATH' with noon service install hint; job env PATH containing tool -> ok.

Commit c7b1fcd on main (fast-forward from worktree branch noon-94-scheduler-path, rebased onto f5ca831). task ci exit 0 on c7b1fcd. Diff within contract; manage_test.go not needed (add covered by AC#1 script). Temporary --temporary add path not checked: outside the settled contract's regular add write; follow-up if wanted. Doctor adds a service_path warn check when the installed unit's PATH can't be parsed. Review: one self-review pass, no defects found.

## Summary

Added service.InstalledPath (parses PATH from the installed launchd/systemd unit). noon doctor resolves job commands with the unit's PATH replacing the inherited PATH and fails with a scheduler-PATH hint; noon add warns on stderr / JSON warnings when the new job's executable is not found, still writing the job. Commit c7b1fcd; task ci green.
