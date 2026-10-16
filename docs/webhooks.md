# Signed webhooks

Noon accepts signed HTTP requests as data-only triggers. A request can select only an opted-in
project and job endpoint; it cannot choose a command or arguments. The payload is stored as an event
file and exposed to the job through `NOON_EVENT_FILE`.

## Configure a job

Set a secret environment-variable name on a job in `noon.yaml` or `.noon.yaml`:

```yaml
version: 1
jobs:
  review:
    run: [task, test]
    webhook:
      secret_env: NOON_GITHUB_WEBHOOK_SECRET
```

The value of `secret_env` is included in the job digest. Store the actual secret outside YAML and
provide it to the scheduler environment. Noon reads a startup snapshot; a missing or empty value
disables that endpoint and is reported as a project config error in `noon status` and `noon doctor`.
For shared jobs, run `noon enable` to approve the definition. A disabled, paused, or unapproved job
will not accept requests.

Enable the project, then inspect JSON output for the route:

```sh
noon enable
noon list --json
```

Webhook jobs include a `webhook_path`, for example `/hooks/12/review`. The integer is the project's
persisted ID. It is available after the project has been registered with `noon enable`.

## GitHub setup

1. In the repository's GitHub settings, add a webhook and set the payload URL to the Noon route
   reachable from GitHub.
2. Set the content type to `application/json` and choose only the events the job needs.
3. Generate a high-entropy secret, enter it in GitHub, and provide the same value as
   `NOON_GITHUB_WEBHOOK_SECRET` to the `noon serve` process.
4. Start Noon with an explicit listener address, for example:

   ```sh
   export NOON_GITHUB_WEBHOOK_SECRET='replace-with-the-GitHub-webhook-secret'
   noon serve --listen 127.0.0.1:8787
   ```

There is no HTTP listener unless `--listen ADDR` is supplied. Choose a loopback address when a
local reverse proxy is available. Binding a non-loopback address logs a warning, but requests still
require a valid HMAC signature. The listener accepts bodies up to 1 MiB and uses a 5s
`ReadHeaderTimeout`, 10s `ReadTimeout` and `WriteTimeout`, and a 16 KiB maximum header size.
`GET /healthz` returns `{"status":"ok"}` while the scheduler loop is healthy. A missing or
incorrect signature receives 401; inactive jobs receive 409.

GitHub sends `X-Hub-Signature-256` as `sha256=HEX`, the HMAC-SHA256 of the exact raw body. A local
request can compute the same signature with OpenSSL:

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

Noon also accepts `X-Noon-Signature-256` and `X-Noon-Delivery`. GitHub delivery IDs become
idempotency keys. Deduplication only covers deliveries whose run is still retained (the newest 50
terminal runs per job); senders without a delivery ID get no replay protection. Job cooldowns and
queued-run coalescing apply as they do to CLI requests.

Event files contain `schema_version`, `source: "webhook"`, `received_at`, parsed JSON `data` (or a
string for non-JSON bodies), and only these request headers: `content-type`, `user-agent`,
`x-github-event`, `x-github-delivery`, and `x-noon-delivery`. Signature and authorization headers
are never stored.

## TLS reverse proxy

Noon does not terminate TLS. Put a reverse proxy in front of a loopback listener and configure the
webhook URL to use HTTPS. For example, Caddy can manage certificates and proxy the request body to
Noon:

```caddyfile
hooks.example.com {
    reverse_proxy 127.0.0.1:8787
}
```

Keep the Noon listener on loopback and protect the secret in the service manager or process
environment. Do not expose the unsigned health endpoint or webhook listener directly to the public
internet without a TLS-terminating reverse proxy.

## Docker port publishing

Bind the container listener to its interface, but publish the host port only on loopback:

```sh
docker run --rm \
  -p 127.0.0.1:8787:8787 \
  -e NOON_GITHUB_WEBHOOK_SECRET \
  noon:dev serve --listen 0.0.0.0:8787
```

The container must also have the enabled project and persistent Noon state mounted as appropriate
for the deployment. Configure TLS at a reverse proxy outside the container. Publishing a host port
on `127.0.0.1` does not provide a public GitHub endpoint by itself; use an appropriately protected
public reverse proxy or load balancer.
