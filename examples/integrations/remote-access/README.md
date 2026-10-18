# Remote jobs over HTTPS

**Level:** intermediate · **Shows:** authenticated JSON API, HTTP MCP, Cloudflare Tunnel, optional ngrok

Run approved jobs on another machine without giving clients a shell. Start locally, then put a
**named Cloudflare Tunnel** in front of the same listener. The optional ngrok path uses the same
API and MCP clients. Neither tunnel installs or starts Noon for you.

```text
CLI / MCP client → HTTPS tunnel hostname → cloudflared → Noon on 127.0.0.1:8787
```

Requires Noon, `jq`, `curl` (7.76+), and `openssl` for the local walkthrough. Tunnel tools and
accounts are only needed for public access. The sole job prints JSON; payloads are never executed.

## 1. Local server, isolated from your normal scheduler

From the Noon repository root, in a fresh terminal:

```sh
noon_demo=$(mktemp -d)
umask 077
cp -R examples/integrations/remote-access "$noon_demo/project"
export HOME="$noon_demo/home" XDG_CONFIG_HOME="$noon_demo/config"
export XDG_STATE_HOME="$noon_demo/xdg-state" NOON_STATE_DIR="$noon_demo/state"
mkdir -p "$HOME" "$XDG_CONFIG_HOME"
openssl rand -hex 32 > "$noon_demo/api-token"
printf 'Demo directory: %s\n' "$noon_demo"
noon -C "$noon_demo/project" config check
noon -C "$noon_demo/project" enable
noon serve --listen 127.0.0.1:8787 --api-token-file "$noon_demo/api-token"
```

Leave this foreground process running. It has its own HOME/config/state, so it serves only the
example project, not your real enabled repositories. The token is read from the protected file,
**not exported into the server environment** that jobs inherit. Use a different port throughout
if 8787 is occupied.

For a real deployment, retain the state and secret files in durable private directories. Run
`serve` under your process supervisor; `noon service install` does not configure these listener
flags. Do not replace the isolation above with your normal state without reviewing every enabled
project: the one shared token grants access to **all enabled projects**, not just this example.

## 2. Discover, queue, and read logs

In a second terminal, set `noon_demo` to the printed absolute directory. This client terminal
must not inherit the server terminal's scratch HOME/XDG variables.

```sh
noon_demo=/absolute/path/printed/by/the/server
noon_url=http://127.0.0.1:8787

# No token: expect 401, not a project listing.
curl -q --silent --output /dev/null --write-out '%{http_code}\n' "$noon_url/v1/projects"

# Keep the expanded Authorization header out of command arguments and shell history.
umask 077
{ printf 'Authorization: Bearer '; cat "$noon_demo/api-token"; } > "$noon_demo/api-header"
noon_api() {
  curl -q --fail-with-body --silent --show-error --max-time 75 \
    --header @"$noon_demo/api-header" "$@"
}

noon_api "$noon_url/v1/projects" | jq .
noon_project_id=$(noon_api "$noon_url/v1/projects" | jq -er '.projects | if length == 1 then .[0].id else error("select a project explicitly") end')
noon_api "$noon_url/v1/projects/$noon_project_id/jobs" | jq .

noon_api --header 'Content-Type: application/json' \
  --data '{"key":"demo-first","data":{"message":"hello from the API"}}' \
  "$noon_url/v1/projects/$noon_project_id/jobs/show-event/runs" > "$noon_demo/queued.json"
jq '{created, reason, run_id: .run.id}' "$noon_demo/queued.json"
noon_run_id=$(jq -er '.run.id' "$noon_demo/queued.json")
noon_api "$noon_url/v1/runs/$noon_run_id?wait_seconds=30&log_bytes=4096" | jq '{run, log}'
```

The queue request returns HTTP **202** with `created: true`. The read should show
`run.status: succeeded` and a log containing `{"message":"hello from the API"}`. If still queued
or running, read the same run again. Repeating the POST with the same key returns **200** with
`created: false`, `reason: duplicate`, and the original ID while that run is retained. Use a new
key for new work. The read's `wait_seconds` is bounded, not a success guarantee.

The `noon_api` function deliberately does not follow redirects or print request headers. Do not
add verbose tracing, shell `set -x`, or redirect following to commands carrying the secret.
Only send it to your own loopback listener or trusted HTTPS hostname.

### Expected refusals

- Missing or incorrect bearer token: **401** on `/v1/` and `/mcp`.
- Changing the approved job definition locally: queueing returns **409** until you review and
  run `noon -C PROJECT enable` on the server again. A remote client cannot approve it.
- A disabled/unknown project or run: **404**. Disabled projects disappear from discovery.
- No add, edit, enable, or approve API exists. `/healthz` is unauthenticated and only checks health.

## 3. Preferred public deployment: named Cloudflare Tunnel

Use a normal terminal on the **server host**, not the scratch server environment from step 1.
Install `cloudflared`, use a domain managed in your Cloudflare account, and authenticate locally.
These commands create account resources; run them only when ready to publish the endpoint.

```sh
cloudflared tunnel login
cloudflared tunnel create noon-demo
```

Copy [`cloudflared.yaml`](cloudflared.yaml) to a private location outside the repository (for
example `/absolute/private/noon-tunnel.yaml`). Replace the tunnel UUID, credentials-file path
printed by `create`, and `noon.example.com` with your own hostname. All paths must be absolute;
never commit the credentials JSON, account `cert.pem`, Noon token, or expanded API header.

```sh
cloudflared tunnel --config /absolute/private/noon-tunnel.yaml ingress validate
cloudflared tunnel --config /absolute/private/noon-tunnel.yaml ingress rule https://noon.example.com/mcp
cloudflared tunnel route dns noon-demo noon.example.com
cloudflared tunnel --config /absolute/private/noon-tunnel.yaml run noon-demo
```

The template routes only the chosen hostname to `http://127.0.0.1:8787`; unmatched hostnames get
404. Keep Noon on loopback: no inbound port forwarding is needed. The tunnel terminates public
HTTPS and connects to the local HTTP listener. It does **not** replace Noon's bearer token.

On the client, set `noon_url=https://noon.example.com` and repeat step 2's discovery/queue/read
commands with a new request key. On a different client machine, securely provision the token
there and create its protected API header file locally; server filesystem paths are not client
paths. Do not use plaintext HTTP over a network.

Use a **named tunnel**, not `cloudflared tunnel --url ...` Quick Tunnels: Quick Tunnels have
changing URLs, no production uptime guarantee, and do not support SSE. HTTP MCP clients may use
SSE, so Quick Tunnels are not the general MCP deployment path.

### Optional Cloudflare Access

Noon authentication is mandatory even with Access. For another perimeter, create an Access
application with a **Service Auth** policy and a service token for unattended clients. A browser
login policy is not sufficient for headless API/MCP clients. Send both:

- `Authorization: Bearer ...` for Noon;
- `CF-Access-Client-Id` and `CF-Access-Client-Secret` for Access.

For curl, put the extra headers into the same protected `api-header` file. Do not configure
Access to consume the `Authorization` header: Noon already needs it. Verify that your MCP client
can supply the two extra headers before enabling Access; the bearer-only Codex setup below does
not configure them. Do not weaken an Access policy just to make a client connect.

## 4. HTTP MCP clients

Use the same HTTPS hostname with `/mcp`. In a **client-only** shell, provision the secret at a
private local path and register Codex:

```sh
export NOON_API_TOKEN="$(cat /secure/path/noon-api-token)"
codex mcp add noon --url https://noon.example.com/mcp --bearer-token-env-var NOON_API_TOKEN
codex
```

The variable must be available whenever the client connects. Do not export it in the scheduler's
shell. See [remote MCP setup](../../../docs/agents.md#remote-mcp-and-json-api) for Claude Code
and its stored-header privacy caveat.

Ask the agent to call `list_projects`, then `list_jobs` with the returned **server-side absolute
project path**. Call `run_job` with that path, `name: show-event`, and JSON `data`; follow with
`get_run` using the returned run ID and bounded `wait_seconds`. HTTP MCP has no default project.
Treat only `succeeded` as completed work, not `noop`, queued, or cancelled results.

## Alternative: ngrok

No separate Noon configuration is needed. Install/authenticate the ngrok agent using its official
setup, then, on the server host while the same loopback Noon listener is running:

```sh
ngrok http http://127.0.0.1:8787 --inspect=false
```

Use the **HTTPS forwarding URL** ngrok prints for `noon_url` and append `/mcp` for MCP clients.
For a stable hostname assigned to your account, add `--url https://YOUR-DOMAIN.ngrok.app`.
Keep Noon's bearer authentication enabled. `--inspect=false` disables the agent's local HTTP
inspection; it is not a promise that the provider retains no traffic. Review provider-side
logging/Traffic Inspector settings before sending sensitive logs. Free-plan limits and endpoint
availability may differ; this is an optional transport, not a dependency of the example.

Both providers terminate TLS and can see requests, credentials, and returned logs. Use SSH
forwarding or a private network instead if that trust is unacceptable. Clients still send the
Noon bearer token through the private connection.

## Stop and verify

Stop the tunnel and Noon with Ctrl-C in their terminals. No service was installed. Named tunnel
and DNS resources remain in your account; remove them through the provider's management tools
when no longer needed. Close the scratch server terminal to discard its environment; its temp
files remain for inspection and contain secrets. Do not publish them.

Local automated verification (no provider credentials, public listener, or real model calls):

```sh
task cli:test PKG=./integration -- -run '^(TestExamplesValidate|TestExampleRemoteAccess|TestRemoteAPI)$' -count=1 -v
```

The example test executes the shipped job through the local API; `TestRemoteAPI` exercises
bearer failures, enabled-project restrictions, HTTP MCP, and token hygiene. Public tunnel
connections are **not** exercised by these tests. Verify discovery, queue/read, and MCP through
your own configured tunnel before relying on it.

Vendor references: [named tunnel setup](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/local-management/create-local-tunnel/),
[ingress configuration](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/local-management/configuration-file/),
[Quick Tunnel limits](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/trycloudflare/),
[Access service tokens](https://developers.cloudflare.com/cloudflare-one/access-controls/service-credentials/service-tokens/),
[ngrok CLI](https://ngrok.com/docs/agent/cli). Commands were checked against these references;
this is not a claim of an authenticated provider deployment.
