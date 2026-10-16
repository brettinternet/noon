# Go project

**Level:** intermediate · **Shows:** test/lint/security cadence, `cooldown`, agent-facing descriptions, cache warming

| Job | When | Why |
| --- | --- | --- |
| `test` | weekdays 9am, plus on demand | Catch breakage from overnight merges; agents run it to verify |
| `race` | nightly 1am | Too slow for the inner loop, too valuable to skip |
| `vet` | every 3h on weekdays | `go vet` and `go mod tidy -diff` drift |
| `vulncheck` | Monday 8am | govulncheck finds reachable CVEs |
| `outdated` | Monday 8:15am | Direct dependencies with updates; changes nothing |
| `warm` | weekdays 8:30am | Fill caches before you sit down; skipped if missed |

```sh
cp examples/local-dev/go-project/noon.yaml ~/src/my-service/noon.yaml
cd ~/src/my-service
noon list
noon run test               # foreground; returns go test's exit code
noon enable && noon serve   # schedules; or noon service install
```

Pair with [git hooks](../git-hooks/) to queue `test` after each commit, with
[failure triage](../../ai/failure-triage/) to diagnose red runs automatically, and with
[notifications](../../integrations/notifications/) to hear about failures.

The `description` on `test` is what coding agents see in `noon list --json` and MCP
`list_jobs`; see [agent verify loop](../../ai/verify-loop/).
