# Wake-ups versus individual events

**Level:** beginner · **Shows:** `requests: merge|each`, event payloads, idempotency keys

Requires Noon and `jq`. Both jobs only print JSON; neither starts on a schedule.

## Queue while the scheduler is offline

From the Noon repository root, use a fresh terminal and isolated config/state. Keep this terminal
open for the whole walkthrough. No service is installed, and your normal scheduler cannot see
these requests.

```sh
noon_demo=$(mktemp -d)
cp -R examples/basics/event-requests "$noon_demo/project"
export HOME="$noon_demo/home" XDG_CONFIG_HOME="$noon_demo/config"
export XDG_STATE_HOME="$noon_demo/xdg-state" NOON_STATE_DIR="$noon_demo/state"
mkdir -p "$HOME" "$XDG_CONFIG_HOME"
cd "$noon_demo/project"
noon enable

noon run refresh --detach --key refresh-first --data @fixtures/first.json --json
noon run refresh --detach --key refresh-second --data @fixtures/second.json --json

noon run event --detach --key event-first --data @fixtures/first.json --json
noon run event --detach --key event-second --data @fixtures/second.json --json
noon run event --detach --key event-first --data @fixtures/second.json --json

noon history --status queued
```

Expected:

| Request | Result |
| --- | --- |
| First `refresh` | `created: true` |
| Second `refresh` | Same run ID; `created: false`, `reason: queued`; second payload discarded |
| First and second `event` | Two different run IDs; both `created: true` |
| Repeated `event-first` key | Original first run ID; `created: false`, `reason: duplicate`; original payload unchanged |

There are **three queued runs**, not five. A key deduplicates only while its run is retained.
Run this walkthrough with fresh state each time.

## Dispatch and inspect

In the same terminal:

```sh
noon serve
```

After the three runs finish, press Ctrl-C, then inspect:

```sh
noon history
noon logs refresh
noon logs event
# Use the first event run ID from history to inspect its original payload too:
noon logs 2
```

In this fresh state, runs 1, 2, and 3 succeed. The refresh log and run 2 log contain
`{"message":"first event"}`; the latest event log contains `{"message":"second event"}`.
The script reads the `data` member of Noon's event envelope, not the whole envelope.

## Choosing a mode

- **Merge:** requests mean “check current state again.” A real refresh job should reconcile
  current state rather than depend on every payload. Existing Git-hook and failure-triage
  examples deliberately keep this behavior.
- **Each:** each request carries distinct work. It preserves payloads, not a delivery guarantee:
  due-now requests are refused with `queue_full` at **20 queued runs per job**, including future
  runs. Work more than **one hour overdue expires**. Reconcile outstanding work if loss matters.
- `cooldown` cannot be combined with `requests: each`. Future `--in`/`--at` requests are already
  independent in either mode. `overlap` is not a substitute for `requests`.

Nothing is left running after Ctrl-C. Close the scratch terminal to discard its environment;
the temporary files remain for inspection. See [Triggers](../../../docs/triggers.md).
