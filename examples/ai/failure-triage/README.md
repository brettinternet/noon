# Automatic failure triage

**Level:** advanced · **Shows:** `on_failure` hooks that queue jobs, hooks as wake-up signals, reconciling against `noon history`, bounded log reads, prompt-injection hygiene

```text
test fails at 2am --on_failure--> noon run triage --detach
triage: noon history --status failed,timed_out -> runs without a report
        -> noon logs ID --section job --tail 300 -> agent -> .noon-triage/test-41.md
triage succeeds --on_success--> desktop notification with the newest report's summary
```

When you sit down, the diagnosis is waiting:

```sh
noon history --status failed
noon logs triage                  # or open .noon-triage/test-41.md
```

Setup: copy `noon.yaml` and `scripts/` into the project, ignore `.noon-triage/` in Git, then
`noon enable` and run a scheduler. Requires `jq`. Try it without waiting for a real failure:

```sh
noon run test        # make it fail once; the hook queues triage
noon logs triage --follow
```

Design notes:

- **The hook is only a wake-up.** If `test` and `e2e` both fail while a triage run is already
  waiting, Noon gives the second request that same waiting run rather than queueing another. So
  triage does not diagnose "the run that called me". It diagnoses every recent failure without a
  report, and a merged request is harmless because the waiting run has not started yet. Report
  files record what was done; `MAX_TRIAGE` caps one run and the next run continues.
- The script collects each log itself and hands it to the model **with no tools**, so text inside
  a failing log cannot make the agent run commands. The prompt also labels the log as untrusted.
- `--section job` excludes hook output; `--tail` and `--limit-bytes` bound the prompt size.
- Recursion is bounded: `triage` skips its own failures and has no `on_failure` hook, and Noon
  refuses requests at depth 3 or more anyway.
