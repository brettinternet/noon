# Schedules

**Level:** beginner · **Shows:** calendar phrases, cron, macros, intervals, `splay`, `jitter`, `timezone`, `overlap`, `missed`, `timeout`, `enabled`

```sh
noon -C examples/basics/schedules list
```

`list` normalizes each schedule and previews its next run in the project timezone without
executing anything. After `noon enable`, NEXT shows times; before that it shows
`needs approval`:

```text
JOB               SCHEDULE              NEXT            LAST   COMMAND
gym               mon,wed,fri at 18:00  needs approval  never  "echo" "gym"
heartbeat         every 10m             needs approval  never  "echo" "still ...
lunch             every day at 12:00    needs approval  never  "echo" "lunch"
month-start       0 7 1 * *             needs approval  never  "echo" "new mo...
morning           weekdays at 09:30     needs approval  never  "echo" "good m...
nightly           @daily                needs approval  never  "echo" "nightly"
...
```

Splay and jitter push starts forward from the schedule. `list --wide` shows the stable splay
offset (`+`) and the jitter window (`~`); NEXT is the effective start:

```text
JOB      SCHEDULE                  NEXT                LAST   COMMAND
fetch    every day at 07:00 +1m8s  Wed 07:01 (in 12h)  never  "echo" "fetch"
standup  weekdays at 08:50 ~20m    Wed 08:54 (in 14h)  never  "echo" "standup"
```

| Want | Use |
| --- | --- |
| A fixed wall-clock time | Calendar phrase (`weekdays at 9:30am`) or cron |
| Something odd (every 15m during work hours) | Cron |
| "Roughly every N" regardless of clock | Interval (`every 10m`) |
| Skip stale work after sleep | `missed: skip` |
| Never lose an occurrence while busy | `overlap: queue` (at most one waiting) |
| Keep many jobs from starting at once | `splay: 10m` (same offset every time) |
| Avoid a predictable start time | `jitter: 20m` (new offset each occurrence) |

Ambiguous phrases fail with a suggestion rather than a guess: `every day` asks whether you meant
`every day at 9am`, `daily`, or `every 24h`. Six-field cron and `@reboot` are rejected.

Pause without editing YAML: `noon pause heartbeat`, then `noon resume heartbeat` (no catch-up).
`noon pause --all` pauses every job currently in the project; `noon resume --all` resumes them
all, including jobs you paused individually. Jobs added later start unpaused.
