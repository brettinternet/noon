# HTTP listener: signed webhooks and remote API

A signed webhook queues a job. The URL picks the project and job, and the body becomes data; it
never chooses a command. The same listener can also serve the authenticated
[remote API and MCP](cli.md#remote-api-and-mcp-over-http).

## 1. Configure the job

```yaml
jobs:
  review:
    run: [task, test]
    webhook:
      secret_env: NOON_GITHUB_WEBHOOK_SECRET
```

`secret_env` names the scheduler's environment variable holding the secret. It's in the approval
digest, so run `noon enable` after adding it.

## 2. Find the URL

```console
$ noon enable
$ noon list --json | jq -r '.jobs[] | select(.webhook_path) | .webhook_path'
/hooks/12/review
```

`12` is the project's ID, assigned by `noon enable`.

## 3. Start the listener

```sh
export NOON_GITHUB_WEBHOOK_SECRET='the-secret-you-gave-github'
noon serve --listen 127.0.0.1:8787
```

Add `--api-token-file FILE` to also serve the remote API. The secret is read once at startup; if
it's missing or empty, that endpoint is off and `noon status` and `noon doctor` report a config error.

## 4. Add the GitHub webhook

In the repository settings, add a webhook with:

- Payload URL: your public HTTPS URL ending in `/hooks/12/review`
- Content type: `application/json`
- Secret: the same high-entropy value
- Events: only the ones the job needs

## Test locally

```sh
body='{"action":"opened"}'
signature=$(printf '%s' "$body" | openssl dgst -sha256 -hmac "$NOON_GITHUB_WEBHOOK_SECRET" -hex | awk '{print $NF}')
curl --fail-with-body -i \
  -H 'Content-Type: application/json' \
  -H 'X-GitHub-Event: issues' \
  -H 'X-GitHub-Delivery: local-test-001' \
  -H "X-Hub-Signature-256: sha256=$signature" \
  --data-binary "$body" \
  http://127.0.0.1:8787/hooks/12/review
```

| Response | When |
| --- | --- |
| 202 | Queued: `{"run_id":7,"created":true}` |
| 200 | Matched an existing run (redelivery, cooldown, or an already-due request in merge mode) |
| 401 | Missing or wrong signature |
| 409 | Project or job disabled, job paused, job needs approval, or the job's platform constraints don't match (code `unsupported_platform`) |
| 413 | Body over 1 MiB |
| 429 | `{"error":"job queue is full"}` for a due-now request at the `requests: each` cap |

`GET /healthz` returns `{"status":"ok"}` while the scheduler is healthy; it needs no auth.

## Request handling

- The signature is HMAC-SHA256 of the raw body, in `X-Hub-Signature-256` or
  `X-Noon-Signature-256`, formatted `sha256=HEX`.
- `X-GitHub-Delivery` or `X-Noon-Delivery` becomes the idempotency key, so a redelivery returns
  the retained run (`reason: duplicate`). Without one there's no replay protection.
- Cooldown, `requests: merge`/`each`, and the queue cap work as for [CLI requests](triggers.md).
- Limits: 1 MiB body, 16 KiB headers, 5s to read headers, 10s to read and write.

The job's `$NOON_EVENT_FILE` has `source: "webhook"`, the parsed JSON body as `data` (a string if
it isn't JSON), and only these headers: `content-type`, `user-agent`, `x-github-event`,
`x-github-delivery`, `x-noon-delivery`. Signatures and authorization headers are never stored.

## TLS reverse proxy

Noon has no TLS or CORS. Keep it on loopback behind a proxy:

```caddyfile
hooks.example.com {
    reverse_proxy 127.0.0.1:8787
}
```

Binding a non-loopback address logs a warning. Never expose the listener without TLS in front.

## Docker

Listen on all interfaces inside the container, but publish only on host loopback:

```sh
docker run --rm \
  -p 127.0.0.1:8787:8787 \
  -e NOON_GITHUB_WEBHOOK_SECRET \
  noon:dev serve --listen 0.0.0.0:8787
```

Mount the project and state as in [Docker](docker.md), and put the TLS proxy outside the
container.
