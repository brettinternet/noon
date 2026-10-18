---
id: NOON-58
title: Demonstrate external heartbeat monitoring of jobs and the scheduler
status: To Do
assignee: []
created_date: '2026-10-18 16:24'
updated_date: '2026-10-18 16:31'
labels:
  - examples
  - integration
  - hooks
dependencies: []
documentation:
  - docs/hooks.md
  - examples/integrations/notifications/README.md
  - docs/service.md
modified_files:
  - examples/integrations/heartbeat/**
  - examples/README.md
  - integration/examples_test.go
priority: low
type: task
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a runnable example shows how to get alerted when Noon work stops happening, including when the scheduler itself is down, using an external dead man switch monitor and only existing hooks and jobs.

Why: lifecycle hooks fire only while a scheduler runs. A crashed service, a reboot without `noon service install`, or a job that silently stops being scheduled produces no alert. Prior art: Healthchecks.io and Cronitor (ping on success, ping a failure endpoint on failure, alert when no ping arrives within period plus grace). decision-4: outbound HTTP is a command, not a built-in action.

Scope: new examples/integrations/heartbeat/ with README.md, noon.yaml, user-config.yaml, and one executable bin/noon-heartbeat.
- `noon-heartbeat URL_FILE` reads one URL from a protected file (the ping URL is a secret; keep it out of YAML and the service unit until draft-9 lands), appends `/fail` when NOON_STATUS is failed or timed_out (Healthchecks.io convention), and sends one bounded request with curl (`-fsS`, a max time, a few retries). It exits nonzero on a missing or unreadable URL file without printing the URL.
- noon.yaml: per-job pings from on_success and on_failure for one job.
- user-config.yaml: a scheduler-level heartbeat job (e.g. `every 5m`, `missed: skip`) in the user project.
- README: monitor period and grace guidance for laptops that sleep (missed runs, catch-up), that a stopped scheduler surfaces only as missing pings, how to point the URL at other providers that accept a plain GET, and the protected-file setup (`umask 077`). Add a row to examples/README.md under integrations.

Non-goals: a built-in HTTP or monitoring action, a provider catalog or Cronitor-specific query parameters, Noon-side alerting when Noon is down, core or docs/ changes.

Dependencies: none. draft-9 may later replace the URL file.

Modified-file contract: the modified_files list plus this task record. Justify other paths in Implementation Notes first. Tests never touch the network: use a local httptest server and isolated HOME, XDG, and state.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `task cli:test -- -run "^TestExamplesValidate$" -count=1` exits 0 and validates the new noon.yaml and user-config.yaml.
- [ ] #2 `task cli:test -- -run "^TestExampleHeartbeat$" -count=1 -v` exits 0 and prints PASS; through the built binary and a copied example, a succeeding job hook hits the local server at the URL path, a failing job hook hits the same path plus /fail, the heartbeat job hits the plain path, and a missing URL file makes the script exit nonzero without the URL in its output.
- [ ] #3 `test -x examples/integrations/heartbeat/bin/noon-heartbeat` exits 0 and `rg -n "integrations/heartbeat" examples/README.md` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 The diff stays within the modified-file contract, or Implementation Notes justify each deviation
- [ ] #4 No test was deleted, skipped, or weakened
- [ ] #5 README, docs, and command help describe only implemented behavior; docs/design.md matches any contract the task settled
<!-- DOD:END -->
