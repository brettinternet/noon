---
id: NOON-102
title: Fire an on_noop hook when a run ends as noop
status: done
type: feature
priority: medium
labels: [hooks]
created: 2026-11-24T19:43:17Z
updated: 2026-11-24T19:50:35Z
---

Why: hooks fire only for succeeded, failed, and timed_out runs. Dead man's switch monitors (Healthchecks.io, see examples/integrations/heartbeat and NOON-58) need a ping whenever a job ran correctly, and a noop (nothing to do) is a correct run. Jobs that mostly noop, such as a 15-minute triage job, otherwise look dead and alert continuously. decision-4: outbound integration is lifecycle hooks; this adds one event to that contract and keeps one hook per run.

Outcome: `on_noop` is a third lifecycle hook selected when a run's status is `noop`. It has the same shape, environment, stdin, timeout, output capture, log section, and default/override rules as `on_success` and `on_failure`.

Settled contract:
- Config: `on_noop` is a job field and a user-config/--config default, parsed and opted out (`[]`) exactly like the other hooks. A job-level `on_noop` requires `noop_exit_code`. Digest rules match the other hooks (job-level value always; defaults only from an approval-gated --config file).
- Execution: status `noop` selects `on_noop`. `NOON_STATUS=noop`, `NOON_EXIT_CODE` is the actual exit code. `NOON_CONSECUTIVE_FAILURES`/`consecutive_failures` reports the existing streak unchanged (noops neither count nor reset). `previous_status` semantics are unchanged.
- Store: hook event validation accepts `on_noop`; history maps status `noop` to event `on_noop`. No schema migration: the event is derived from run status.
- Docs: hooks table row for noop, configuration hook fields, design hook contract, JSON schema, and the heartbeat example README notes that jobs with `noop_exit_code` should also set `on_noop` to the heartbeat.

Non-goals: an always-run `on_finish` hook or multiple hooks per run; hooks for cancelled/skipped/interrupted runs; a built-in HTTP or monitoring action; changes to `previous_status` semantics.

Dependencies: none. Follows decision-4.

Modified-file contract: internal/config/**, internal/app/hooks.go, internal/app/hooks_test.go, internal/app/history.go, internal/app/manage.go, internal/cli/manage.go, internal/cli/verify.go, internal/store/store.go, noon.schema.json, docs/hooks.md, docs/configuration.md, docs/design.md, examples/integrations/heartbeat/README.md, plus this task record. Justify other paths in Notes first.

## Acceptance criteria

- [x] `task cli:test PKG=./internal/config -- -count=1` exits 0: `on_noop` parses at job level and as a user-config or --config default, is rejected as a shared/project-level default, an empty list opts a job out of the default, a job-level `on_noop` without `noop_exit_code` is an error, and `on_noop` joins the digest under the same rules as `on_success`/`on_failure`.
  Evidence: task cli:test PKG=./internal/config -- -count=1 → ok. TestLoadJobs covers on_noop user default + [] opt-out, job on_noop without noop_exit_code error, project-level on_noop default rejected; digest uses the shared hookFields loop.
- [x] `task cli:test PKG=./internal/app -- -count=1` exits 0: a run job whose exit matches `noop_exit_code` runs `on_noop` with `NOON_STATUS=noop`, `NOON_CONSECUTIVE_FAILURES` equal to the retained failure streak (not incremented, not reset), and stdin `run.hook.event` `on_noop`; history JSON reports the hook; `on_success` and `on_failure` do not fire for noops.
  Evidence: task cli:test PKG=./internal/app -- -count=1 → ok. TestNoopHook: after a failed run, a noop runs on_noop (not on_success exit 9) with NOON_STATUS=noop, NOON_EXIT_CODE=3, NOON_CONSECUTIVE_FAILURES=1, NOON_PREVIOUS_STATUS=failed; RunFromStore hook event on_noop status ok.
- [x] docs/hooks.md, docs/configuration.md, docs/design.md, noon.schema.json, and the heartbeat example README describe only implemented behavior. `task ci` exits 0.
  Evidence: docs/hooks.md, configuration.md, design.md, cli.md, noon.schema.json (on_noop + requires noop_exit_code), heartbeat README updated; jq validates schema. bl heartbeat -- task ci → exit 0.

## Definition of done

- [x] task ci passes on the final commit
  Evidence: task ci exit 0 on the tree committed as 51ac1ae (only gofmt-neutral staging after the run)
- [x] Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
  Evidence: AC evidence recorded under each criterion with command and result
- [x] The diff stays within the modified-file contract, or Implementation Notes justify each deviation
  Evidence: docs/cli.md deviation and mechanical test call-site updates justified in Notes
- [x] No test was deleted, skipped, or weakened
  Evidence: No tests deleted, skipped, or weakened; existing hook tests extended
- [x] README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
  Evidence: hooks/configuration/design/cli docs and schema describe only implemented on_noop

## Notes

Deviation: docs/cli.md (outside contract) said noops run no hooks; updated that one sentence so docs describe implemented behavior. Refactor: config.Hooks struct replaces separate OnSuccess/OnFailure fields and default params (embedded in Job and Config) to avoid a third positional any param in JobFromSource/parseJob; test call sites updated mechanically (internal/scheduler/serve_test.go, internal/cli/manage_test.go, internal/config/*_test.go).
