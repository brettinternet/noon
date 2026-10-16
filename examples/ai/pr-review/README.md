# AI pull request review and issue triage (webhooks)

**Level:** advanced · **Shows:** `webhook.secret_env`, `serve --listen`, webhooks as wake-up signals, reconciling against GitHub, opt-in side effects

GitHub sends a signed webhook → Noon verifies the HMAC, stores the payload, and queues the job →
the script asks GitHub what still needs attention, fetches each diff with `gh`, and asks an agent
for a review. Webhooks can select a job, never a command: the payload is only data.

| Job | GitHub event | Default effect | Opt-in effect |
| --- | --- | --- | --- |
| `pr-review` | `pull_request` | Writes `.noon-reviews/<repo>-<n>-<sha>.md` for each open, ready PR whose head has no review | `POST_COMMENT=1`: posts it as a comment |
| `issue-triage` | `issues` | Writes `.noon-issue-triage/<repo>-<n>.md` for each open, unlabeled issue not yet triaged | `APPLY_LABELS=1`: applies labels |

## Why the payload is only a wake-up

When a request arrives while a run of the same job is already **waiting** in the queue, Noon
returns that run instead of queueing another, so its payload is not kept. If a review takes ten
minutes and three PRs open meanwhile, the second and third webhooks share one waiting run.
Reviewing "the PR in this payload" would silently skip one of them.

Instead, every run lists open PRs and reviews each one whose current head commit has no review
file yet. A merged request is harmless because the waiting run has not started and will see every
PR. The review files are the record of what was done; delete one to request a fresh review.
`MAX_REVIEWS` caps the work per run, and the next run continues where it stopped. The same
pattern drives `issue-triage`. Avoid `cooldown` on jobs like these: a request refused by a
cooldown is dropped, and nothing wakes the job again until the next event.

Set `REPOS` (space-separated `owner/name`) when one job serves several repositories; otherwise
the repository comes from the payload.

## Run it locally

```sh
cp -R examples/ai/pr-review/. ~/src/review-bot/ && cd ~/src/review-bot && git init
noon enable
noon list --json | jq -r '.jobs[] | "\(.name) \(.webhook_path)"'
# pr-review /hooks/3/pr-review

export NOON_GITHUB_WEBHOOK_SECRET=$(openssl rand -hex 32)
noon serve --listen 127.0.0.1:8787        # same shell, so it has the secret

# another shell, same secret exported:
scripts/send-test-webhook.sh http://127.0.0.1:8787/hooks/3/pr-review pull_request fixtures/pull_request.json
noon logs pr-review --follow
```

First replace `OWNER/REPO` in `fixtures/*.json` with a repository you can read. The first run
reviews up to `MAX_REVIEWS` of its open PRs.

## Connect GitHub

GitHub must reach the listener. Options, from least to most exposed:

- **`gh webhook forward`** (the `cli/gh-webhook` extension) relays a repository's events to a
  local URL without a public endpoint. Check its `--help` for passing the shared secret: Noon
  rejects unsigned requests.
- A tunnel (Tailscale Funnel, Cloudflare Tunnel) to `127.0.0.1:8787`.
- A TLS reverse proxy on a server; see [webhooks](../../../docs/webhooks.md#tls-reverse-proxy).

In the repository's webhook settings use content type `application/json`, the shared secret, and
only the `Pull requests` or `Issues` event. GitHub's `X-GitHub-Delivery` becomes the idempotency
key, and review files make repeated runs no-ops, so redeliveries do not review twice. For a service install, put the secret in the service
environment rather than a shell profile.

## Safety

The PR text and diff are attacker-controlled for public repositories. The agent gets them as
prompt data with **no tools**, the prompt says to ignore embedded instructions, and posting is off
by default. Keep it that way for repositories that accept outside contributions.
