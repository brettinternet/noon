# Job pipelines with hooks

**Level:** advanced · **Shows:** chaining with `on_success`, `--data -` from hook stdin, run-scoped idempotency keys, recursion depth

```text
build (schedule, depth 0) --on_success--> test (queued, depth 1) --on_success--> package (queued, depth 2)
```

A hook receives the finished run as JSON on stdin. `--data -` stores that JSON as the next job's
event file, so `package` can see which test run produced it:

```sh
noon logs package
# after test run 41: succeeded
# packaged dist/app-v1.4.0-3-gabc1234.tar.gz
```

`--key "after-build-$NOON_RUN_ID"` makes each hand-off idempotent: a retried hook cannot queue
the same stage twice. Failures stop the chain because only `on_success` hooks continue it; add
`on_failure` to alert.

**Depth limit.** Every queued run records the requester's `NOON_DEPTH`; requests at depth 3 or more
fail with code `recursion`. Starting from a scheduled or manual run, a chain can therefore have at
most three stages. Longer workflows belong in one job running a script or task runner, which is
also easier to reason about. A job also cannot queue itself.

Queued stages need `noon enable` and a running `noon serve`. Start the whole chain manually with
`noon run build`.
