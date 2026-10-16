# Signed webhooks

A signed HTTP request can queue a job. The request picks a project and job by URL and supplies
data; it can never choose a command.

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

There is no listener without `--listen`. The secret is read once at startup; if it's missing or
empty, that endpoint is off and `noon status` and `noon doctor` report a config error.

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
| 200 | Matched an existing run (redelivery, cooldown, or already due) |
| 401 | Missing or wrong signature |
| 409 | Project or job disabled, job paused, or job needs approval |
| 413 | Body over 1 MiB |

`GET /healthz` returns `{"status":"ok"}` while the scheduler is healthy.

## Request handling

- The signature is HMAC-SHA256 of the raw body, in `X-Hub-Signature-256` or
  `X-Noon-Signature-256`, formatted `sha256=HEX`.
- `X-GitHub-Delivery` or `X-Noon-Delivery` becomes the idempotency key, so redeliveries don't
  queue twice while the earlier run is retained (newest 50 per job). Without one there's no replay
  protection.
- Cooldown and merging into an already-due run work as for [CLI requests](triggers.md).
- Limits: 1 MiB body, 16 KiB headers, 5s to read headers, 10s to read and write.

The job's `$NOON_EVENT_FILE` has `source: "webhook"`, the parsed JSON body as `data` (a string if
it isn't JSON), and only these headers: `content-type`, `user-agent`, `x-github-event`,
`x-github-delivery`, `x-noon-delivery`. Signatures and authorization headers are never stored.

## TLS reverse proxy

Noon doesn't do TLS. Keep it on loopback behind a reverse proxy:

```caddyfile
hooks.example.com {
    reverse_proxy 127.0.0.1:8787
}
```

Binding a non-loopback address logs a warning; requests still need a valid signature. Don't
expose the listener to the internet without TLS in front.

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
