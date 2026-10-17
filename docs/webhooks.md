# HTTP listener: signed webhooks and remote API

A signed webhook can queue a job. The request picks a project and job by URL and supplies data; it
can never choose a command. The same `serve --listen` listener can optionally expose an authenticated
JSON API and MCP over HTTP. Without `--api-token-file`, it serves only webhooks and `/healthz` as
before. See the [remote route reference](cli.md#remote-api-and-mcp-http) and [agent setup](agents.md#remote-mcp-and-json-api).

## 1. Configure the job

```yaml
jobs:
  review:
    run: [task, test]
    webhook:
      secret_env: NOON_GITHUB_WEBHOOK_SECRET
```

`secret_env` names the environment variable holding the secret; the secret itself never goes in
YAML. The setting is part of the approval digest, so run `noon enable` after adding it.

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

To also enable the remote API and MCP, use the API token file with the same listener instead:

```sh
noon serve --listen 127.0.0.1:8787 --api-token-file /run/secrets/noon-api-token
```

There is no listener without `--listen`. The webhook secret is read once at startup; if it's
missing or empty, that endpoint is off and `noon status` and `noon doctor` report a config error.

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

`GET /healthz` returns `{"status":"ok"}` while the scheduler is healthy and does not require a
bearer token. When configured, `/v1/` and `/mcp` require the token from `--api-token-file`; webhook
requests still require their HMAC signatures. The API token is never passed to jobs or hooks.

## Request handling

- The signature is HMAC-SHA256 of the raw body, in `X-Hub-Signature-256` or
  `X-Noon-Signature-256`, formatted `sha256=HEX`.
- `X-GitHub-Delivery` or `X-Noon-Delivery` becomes the idempotency key, so redeliveries don't
  queue twice while the earlier run is retained (newest 50 per job). Without one there's no replay
  protection.
- With the default `requests: merge`, a delivery that finds an already-due queued run gets 200 and
  its new payload is dropped; the existing run's event file is unchanged. `requests: each` creates
  a separate run for each delivery, while a repeated delivery ID still gets 200 with
  `reason: duplicate`.
- Due-now `each` deliveries have a per-job cap of 20 queued runs, counting due and future work.
  At the cap, a delivery gets 429 and creates neither a run nor an event file. `--in` and `--at`
  one-shots remain independent, and their runs count toward this cap; see [Triggers](triggers.md)
  for queue expiry and handling guidance.
- Cooldown and merging into an already-due run work as for [CLI requests](triggers.md); cooldown
  cannot be configured with `requests: each`.
- Webhook limits remain: 1 MiB body, 16 KiB headers, 5s to read headers, 10s to read and write.
  The optional JSON API limits request bodies to 512 KiB (`data` to 256 KiB) and permits
  `wait_seconds` up to 60 by extending API response write deadlines; webhook time limits are unchanged.

The job's `$NOON_EVENT_FILE` has `source: "webhook"`, the parsed JSON body as `data` (a string if
it isn't JSON), and only these headers: `content-type`, `user-agent`, `x-github-event`,
`x-github-delivery`, `x-noon-delivery`. Signatures and authorization headers are never stored.

## TLS reverse proxy

Noon doesn't do TLS or CORS. Keep it on loopback behind a reverse proxy for webhook and remote API access:

```caddyfile
hooks.example.com {
    reverse_proxy 127.0.0.1:8787
}
```

Binding a non-loopback address logs a warning. API requests need a bearer token and webhook
requests need signatures. Don't expose the listener to the internet without TLS in front.

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
