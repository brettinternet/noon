---
id: decision-5
title: Job semantics follow cron and launchd expectations
date: '2026-10-16 01:14'
status: accepted
---
## Context

The seeded design required an explicit shell action, skipped missed runs by default, rejected
natural-language schedules, and left DST behavior to a future library choice. Laptops sleep through
scheduled times; launchd and Claude Code scheduled tasks both run one catch-up after wake. Cron
users write commands as shell strings. Cron syntax is a common source of mistakes.

## Decision

1. `run:` as a YAML string executes with `/bin/sh -c`; as a list it is exact argv. The YAML type
   decides, never punctuation.
2. Missed runs default to `run_once`: one catch-up run, never a burst, recorded against the first
   missed occurrence so no unbounded counting is needed. `missed: skip` opts out. New, changed,
   resumed, and newly enabled jobs start from now.
3. Overlap defaults to `skip`; `queue` keeps one pending run. Default timeout is 1h, `none` opts
   out, so a hung job cannot silently block every later run.
4. Schedules accept five-field cron, macros, a small deterministic calendar grammar
   (`weekdays at 9am`), and elapsed intervals (`every 6h`). No free text, no LLM parsing.
   Ambiguous phrases (`every day`, `every hour`, `at 9`) are rejected with suggestions. Intervals
   keep the phase they started with; clock alignment is what cron forms are for.
5. Noon owns cron evaluation. Wall-clock rules: a repeated local time runs once at its first
   occurrence; a skipped local time runs once right after the gap; intervals ignore DST.
6. Crontabs are imported into YAML, not read live as a second format.

## Consequences

- Behavior is documented and tested in `internal/schedule` with fixed DST fixtures.
- A 9am job may run at 11pm after a day asleep; time-sensitive jobs set `missed: skip`.
- Cron's `%`, `MAILTO`, `CRON_TZ`, seconds fields, and `@reboot` are rejected on import with line
  numbers rather than approximated.
