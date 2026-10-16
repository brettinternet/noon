# Research digest and release radar

**Level:** intermediate · **Shows:** fetch → filter → summarize pipelines, `NOON_LAST_SUCCESS_AT` windows, catch-up after sleep

| Job | When | Output |
| --- | --- | --- |
| `digest` | daily 7am (once on wake if missed) | `digest/YYYY-MM-DD.md`: HN + arXiv picks matching `INTERESTS` |
| `release-watch` | weekdays 9am | `digest/releases-YYYY-MM-DD.md`: releases in [`watch.txt`](watch.txt) since the last success |

```sh
noon -C examples/ai/research-digest run release-watch
noon -C examples/ai/research-digest logs digest
```

Requires `curl`, `jq`, and an authenticated `gh` (for release-watch). Network fetches happen in
the script; the model only ranks and summarizes, with no tools.

`release-watch` uses `NOON_LAST_SUCCESS_AT` as its window, so a day the laptop was closed or the
job failed is covered by the next successful run instead of being lost.
