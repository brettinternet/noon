# Automatic failure triage

**Level:** advanced · **Shows:** `on_failure` hooks that queue jobs, hook stdin as event data, bounded log reads, prompt-injection hygiene

```text
test fails at 2am --on_failure--> noon run triage --detach --data -  (failed run JSON)
triage: jq run id -> noon logs ID --section job --tail 300 -> agent -> .noon-triage/test-41.md
triage succeeds --on_success--> desktop notification with the report's one-line summary
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

- The script collects the log itself and hands it to the model **with no tools**, so text inside a
  failing log cannot make the agent run commands. The prompt also labels the log as untrusted.
- `--section job` excludes hook output; `--tail` and `--limit-bytes` bound the prompt size.
- `overlap: queue` keeps one triage waiting if two jobs fail at once. Noon allows at most one
  waiting run per job, so a third failure during that window is coalesced; every failure is still
  in `noon history`.
- Recursion is bounded: a triage failure cannot trigger another triage because `triage` has no
  `on_failure` hook, and Noon refuses requests at depth 3 or more anyway.
