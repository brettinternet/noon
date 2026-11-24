# Web viewer

The scheduler can serve a read-only web viewer on its HTTP listener: every enabled project, what's
failing, what runs next, and live run logs. It's off unless you give it a web token.

## Enable

Create a token of at least 32 bytes, separate from the API token:

```sh
mkdir -p ~/.config/noon
(umask 077; openssl rand -base64 32 > ~/.config/noon/web-token)
```

Set it in user config next to the listen address:

```yaml
# ~/.config/noon/config.yaml
serve:
  listen: 127.0.0.1:8787
  web_token_file: ~/.config/noon/web-token
```

Then start or restart the scheduler:

```sh
noon service install   # or: noon service restart
noon serve             # or in the foreground
```

The startup log names the URL (`web viewer url=http://127.0.0.1:8787/`). Open it and paste the
token. `noon serve --web-token-file FILE` overrides the setting. `noon config` shows
`Listener: ADDR (web)` and `noon doctor` checks the file. The token is read once at startup;
restart the scheduler after changing it.

A web token without a listen address, or one equal to the API token, is a usage error (exit 2).
An unreadable or short token file stops `serve` with exit 1 before it schedules anything.

## Pages

| Route | Shows |
| --- | --- |
| `/` | Projects with state, next run, and recent runs; failing jobs; runs in the next 24 hours |
| `/projects/{id}` | Jobs and recent runs, filterable by job and status |
| `/runs/{id}` | Run details, the prompt and resume command for prompt jobs, and the log |

Only enabled projects and their runs are reachable; anything else is 404. The log shows its last
256 KiB with an Earlier link, `#L42` links to line 42, and ANSI colors render as colors while
other control sequences are dropped. Pages update live over one server-sent event stream per tab,
closed while the tab is hidden. The tab title and icon show the failing-job count. Times show in
the browser's timezone; without JavaScript they show in the project's timezone.

The viewer never queues, cancels, enables, or approves. Needs-approval projects show the command
to run on the host.

## Web and API tokens

| | Web token | API token |
| --- | --- | --- |
| Setting | `serve.web_token_file` | `serve.api_token_file` |
| Used by | Browsers, through `/login` | `/v1/` and `/mcp` clients, as a bearer token |
| Grants | Reading every enabled project and its logs | Reading, queueing, and cancelling runs |

Either works without the other, and neither is accepted in the other's place. Signing in sets an
`HttpOnly`, `SameSite=Strict` session cookie for 30 days, signed with the web token; it is
`Secure` when the request arrived over HTTPS. Rotate the token and restart to sign every browser
out. Neither token is logged, shown, or passed to jobs or hooks.

## Remote access

Logs can contain secrets, so reach the viewer only over a private link: loopback, SSH forwarding,
a private network, or an HTTPS reverse proxy or tunnel. Never publish the plain HTTP listener.
Noon has no TLS. The proxy must preserve `Host` (cross-origin form posts are refused by comparing
`Origin` with `Host`), set `X-Forwarded-Proto: https`, and not buffer responses, so live updates
arrive.

Caddy does all three by default:

```caddyfile
noon.example.com {
    reverse_proxy 127.0.0.1:8787
}
```

nginx:

```nginx
server {
    listen 443 ssl;
    server_name noon.example.com;
    # ssl_certificate ...;

    location / {
        proxy_pass http://127.0.0.1:8787;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_buffering off;
        proxy_read_timeout 1h;
    }
}
```

The same listener also serves [webhooks](webhooks.md) and, with an API token, the
[remote API and MCP](cli.md#remote-api-and-mcp-over-http). For a Cloudflare Tunnel, see
[remote access](../examples/integrations/remote-access/README.md#web-viewer).
