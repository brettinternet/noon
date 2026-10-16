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

- **`gh webhook forward`** (the `cli/gh-webhook` extension) can relay events without a
  public endpoint. The source revision below supports `--secret`; supply the same secret as
  Noon, e.g. `gh webhook forward --repo OWNER/REPO --events pull_request,issues --url URL --secret "$NOON_GITHUB_WEBHOOK_SECRET"`.
  This creates/activates a development webhook and needs GitHub permission. The secret is
  passed in argv (potentially visible to local process inspection). Check your installed
  extension's help; if it lacks `--secret`, use a tunnel/proxy instead. Never disable HMAC checks.
- A tunnel (Tailscale Funnel, Cloudflare Tunnel) to `127.0.0.1:8787`.
- A TLS reverse proxy on a server; see [webhooks](../../../docs/webhooks.md#tls-reverse-proxy).

In the repository's webhook settings use content type `application/json`, the shared secret, and
only the `Pull requests` or `Issues` event. GitHub's `X-GitHub-Delivery` becomes the idempotency
key, and review files make repeated runs no-ops, so redeliveries do not review twice. For a service install, put the secret in the service
environment rather than a shell profile.

## Safety

The PR text and diff are attacker-controlled for public repositories. The default Claude command
uses `--tools ""` to remove built-in tools and `--disallowedTools 'mcp__*'` to deny MCP tools;
`--strict-mcp-config` excludes ordinary configured servers. `--safe-mode` disables user/project
customizations, including hooks. This is a model tool restriction, **not an OS sandbox**:
the CLI still authenticates and sends the prompt to its model service, managed policy can still
run hooks, and the surrounding script uses GitHub credentials and writes output files. Use a
trusted managed configuration or an isolated runtime without sensitive files for hostile input.

The prompt asks the model to ignore embedded instructions, but that is not a security boundary.
Posting/label changes remain off by default; review generated output before enabling them.
An `AGENT_CMD` override replaces the default safeguards and must establish its own restrictions.
It is a whitespace-split command, not shell syntax; embedded quotes do not preserve empty arguments.

## Verification (2026-10-16)

**Documentation/help/source evidence only; no model-backed review or live GitHub delivery.**
Claude Code **2.1.302** `claude --help` and the
[CLI reference](https://code.claude.com/docs/en/cli-reference) document `--tools ""`,
`--disallowedTools "mcp__*"`, `--strict-mcp-config`, and `--safe-mode` (including managed-policy
exceptions). The [headless reference](https://code.claude.com/docs/en/headless) makes clear that
`--permission-prompts none` only denies unresolved prompts: it does **not** remove Read/Grep/Glob
or tools allowed by settings. Both scripts now pass a real empty argv entry to `--tools`.

Inspected `cli/gh-webhook` through `gh api` at revision
`115d4d6768b55c74ee7d7c61d4775d83fdc323d2`:
[forward.go](https://github.com/cli/gh-webhook/blob/115d4d6768b55c74ee7d7c61d4775d83fdc323d2/webhook/forward.go)
declares `--secret/-S`, forwards `bytes.NewReader(ev.Body)` and copies event headers;
[create_webhook.go](https://github.com/cli/gh-webhook/blob/115d4d6768b55c74ee7d7c61d4775d83fdc323d2/webhook/create_webhook.go)
sends the secret in hook configuration. Thus the client preserves the signature header and
exact body needed by Noon. End-to-end service delivery remains untested; use the local signed
fixture to test Noon, and check actual delivery status before relying on forwarding.
