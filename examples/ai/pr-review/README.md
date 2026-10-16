# AI pull request review and issue triage (webhooks)

**Level:** advanced · **Shows:** `webhook.secret_env`, `serve --listen`, event headers in `NOON_EVENT_FILE`, delivery-ID idempotency, opt-in side effects

GitHub sends a signed webhook → Noon verifies the HMAC, stores the payload, and queues the job →
the script reads the payload, fetches the diff with `gh`, and asks an agent for a review.
Webhooks can select a job, never a command: the payload is only data.

| Job | GitHub event | Default effect | Opt-in effect |
| --- | --- | --- | --- |
| `pr-review` | `pull_request` | Writes `.noon-reviews/<repo>-<n>-<sha>.md` | `POST_COMMENT=1`: posts it as a comment |
| `issue-triage` | `issues` | Prints suggested labels and reply | `APPLY_LABELS=1`: applies labels |

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

The fixture points at a real public PR (`cli/cli#1`), so `gh pr diff` works without access to a
private repository.

## Connect GitHub

GitHub must reach the listener. Options, from least to most exposed:

- **`gh webhook forward`** (the `cli/gh-webhook` extension) relays a repository's events to a
  local URL without a public endpoint; give it the same secret.
- A tunnel (Tailscale Funnel, Cloudflare Tunnel) to `127.0.0.1:8787`.
- A TLS reverse proxy on a server; see [webhooks](../../../docs/webhooks.md#tls-reverse-proxy).

In the repository's webhook settings use content type `application/json`, the shared secret, and
only the `Pull requests` or `Issues` event. GitHub's `X-GitHub-Delivery` becomes the idempotency
key, so redeliveries do not review twice. For a service install, put the secret in the service
environment rather than a shell profile.

## Safety

The PR text and diff are attacker-controlled for public repositories. The agent gets them as
prompt data with **no tools**, the prompt says to ignore embedded instructions, and posting is off
by default. Keep it that way for repositories that accept outside contributions.
