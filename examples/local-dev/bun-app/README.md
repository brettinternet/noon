# Bun web app

**Level:** intermediate · **Shows:** fast vs nightly suites, a budget gate script, env for CI-style tools, manual-only jobs

| Job | When | Notes |
| --- | --- | --- |
| `check` | on demand | Typecheck + unit tests; target for agents and git hooks |
| `e2e` | nightly 2am | Build + Playwright with `CI=1` |
| `outdated`, `audit` | Monday morning | Reports only |
| `bundle-size` | weekdays 6pm | [`scripts/bundle-budget.sh`](scripts/bundle-budget.sh) exits 1 over budget |
| `seed` | manual | Destructive to local data, so it has no schedule |

Assumes `package.json` scripts named `typecheck`, `build`, and `db:seed`.

```sh
noon run check
noon run bundle-size
noon logs bundle-size --status failed --tail 20
```

A failing exit code is how a job raises an alarm: combine `bundle-size` with an `on_failure`
[notification](../../integrations/notifications/).
