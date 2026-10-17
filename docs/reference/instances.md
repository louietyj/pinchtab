# Instances

Instances are running Chrome processes managed by PinchTab. Each managed instance has:

- an instance ID
- a profile
- a port
- a mode (`headless` or `headed`)
- an execution status

One profile can have at most one active managed instance at a time.

## List Instances

```bash
curl http://localhost:9867/instances
# Response: JSON array (see below)

# CLI Alternative (human-readable by default)
pinchtab instance list
# Output (tab-separated): inst_0a89a5bb  9999  headed  running

pinchtab instance list --json          # Full JSON response
```

`pinchtab instance list` is the simplest way to inspect the current fleet from the CLI. The older `pinchtab instances` spelling still runs as a deprecated alias.

Response shape:

```json
[
  {
    "id": "inst_0a89a5bb",
    "profileId": "prof_278be873",
    "profileName": "instance-1741410000000000000-3fa9c2d1",
    "port": "9999",
    "mode": "headed",
    "headless": false,
    "status": "running",
    "startTime": "2026-03-08T05:00:00Z",
    "attached": false,
    "responsiveness": "responsive",
    "securityPolicy": {
      "allowedDomains": ["127.0.0.1", "localhost", "::1", "wikipedia.org"]
    }
  }
]
```

`GET /instances` returns a bare JSON array, not an envelope like `{"instances":[...]}`. Each instance response includes both `mode` (`"headless"` or `"headed"`) and the legacy-compatible `headless` boolean.
Optional fields appear when they apply: `url` (bridge-backed instances), `error` (when `status` is `error`), `attachType` and `cdpUrl`
(attached instances), `browser`, `fallbackFrom`/`fallbackReason` (a launch that fell back to another target), and `crashes`.

## Start An Instance

### `POST /instances/start`

Use `/instances/start` when you want to start by profile ID or profile name, or let PinchTab create a temporary profile.

```bash
curl -X POST http://localhost:9867/instances/start \
  -H "Content-Type: application/json" \
  -d '{"profileId":"prof_278be873","mode":"headed","port":"9999","securityPolicy":{"allowedDomains":["wikipedia.org","wikimedia.org"]}}'
# CLI Alternative
pinchtab instance start --profile prof_278be873 --mode headed --port 9999 --allow-domain wikipedia.org --allow-domain wikimedia.org
```

Request body:

- `profileId`: optional; accepts a profile ID or an existing profile name
- `mode`: optional; use `headed` for a visible browser, anything else is treated as headless
- `port`: optional
- `securityPolicy.allowedDomains`: optional additive instance-scoped IDPI/domain allowlist entries
- `browser`: optional provider or `browser.targets` name (CLI `--browser`)
- `fallbackTargets`: optional ordered target names to try if the first launch fails (CLI `--browser-fallback`, repeatable)

The response is `201` with the instance object.

Notes:

- if `profileId` is omitted, PinchTab creates an auto-generated temporary profile
- if `port` is omitted, PinchTab allocates one from the configured instance port range
- the CLI flag is `--profile`, even though the API field is `profileId`
- `securityPolicy.allowedDomains` is merged with the server-level `security.allowedDomains` baseline for that instance only
- you can widen a single instance without changing the server default. For example, `{"securityPolicy":{"allowedDomains":["*"]}}` makes that instance unrestricted while other instances still use the server baseline
- request-supplied extension paths are rejected (so `pinchtab instance start --extension` fails with `400`);
  configure `browser.extensionPaths` on the server instead. By default, PinchTab uses the local `extensions/` directory under its state/config folder.

### `POST /instances/launch`

`/instances/launch` is a compatibility alias for `/instances/start`.

```bash
curl -X POST http://localhost:9867/instances/launch \
  -H "Content-Type: application/json" \
  -d '{"profileId":"prof_278be873","mode":"headed","securityPolicy":{"allowedDomains":["wikipedia.org"]}}'
```

Request body:

- `profileId`: optional existing profile ID or existing profile name
- `mode`: optional; `headed` or headless by default
- `port`: optional
- `securityPolicy.allowedDomains`: optional additive instance-scoped IDPI/domain allowlist entries
- `browser`, `fallbackTargets`: optional, as for `/instances/start`

Important:

- `/instances/launch` does not read a `headless` field. Use `mode:"headed"` when you want a headed browser.
- `name` is no longer supported on `/instances/launch`. Create the profile first via `POST /profiles`, then use the returned `id` as `profileId`.
- request-supplied extension paths are rejected; configure `browser.extensionPaths` on the server instead. By default, PinchTab uses the local `extensions/` directory under its state/config folder.

## Get One Instance

```bash
curl http://localhost:9867/instances/inst_ea2e747f
```

Common status values:

- `starting`
- `running`
- `stopping`
- `stopped`
- `error`

Instance responses include:

- `mode`: `"headless"` or `"headed"`
- `headless`: boolean kept for compatibility
- `responsiveness`: whether the instance's browser routes answer, measured by the latest completed probe of the instance's `/tabs` under a short budget, which a front-door `/health` or monitoring snapshot starts and waits for at most 250ms; a slower probe is reported by the next read. `responsive` when it answered, `unresponsive` when its `/health` answered but `/tabs` did not within the budget, `unknown` when it has not been probed yet or the probe could not connect. It never changes `status`, which stays `running` for an unresponsive instance, and it triggers no restart. The front door's `/health` degrades while any instance is `unresponsive`

## Get Instance Logs

```bash
curl http://localhost:9867/instances/inst_ea2e747f/logs
# CLI Alternative
pinchtab instance logs inst_ea2e747f
```

Response is plain text. There is also an SSE stream at `GET /instances/{id}/logs/stream`.

## Stop An Instance

```bash
curl -X POST http://localhost:9867/instances/inst_ea2e747f/stop
# CLI Alternative
pinchtab instance stop inst_ea2e747f
```

Stopping an instance preserves the profile unless it was a temporary auto-generated profile.
The response is `{"status":"stopped","id":"<instanceId>"}`.

## Restart Or Start An Instance By ID

```bash
curl -X POST http://localhost:9867/instances/inst_ea2e747f/restart
# CLI Alternative
pinchtab instance restart inst_ea2e747f
```

`POST /instances/{id}/restart` soft-restarts the browser process of a running instance
(`503` when it is not running). `POST /instances/{id}/start` starts a known, stopped
instance again with its previous profile, port and mode (`201` with the instance), or
ensures the browser of one that is still active; attached CDP instances cannot be started
this way (`409`).

## Start By Profile

You can also start an instance from a profile-oriented route:

```bash
curl -X POST http://localhost:9867/profiles/prof_278be873/start \
  -H "Content-Type: application/json" \
  -d '{"headless":false,"port":"9999","securityPolicy":{"allowedDomains":["wikipedia.org"]}}'
```

This route accepts a profile ID or profile name in the path. Unlike `/instances/start` and `/instances/launch`, its request body uses `headless` instead of `mode`.
`headless` defaults to `false`, so a request without it starts a headed browser. The body also accepts
`browser` and `fallbackTargets`.

## Open A Tab In An Instance

```bash
curl -X POST http://localhost:9867/instances/inst_ea2e747f/tabs/open \
  -H "Content-Type: application/json" \
  -d '{"url":"https://pinchtab.com"}'
```

There is no dedicated instance-scoped `tab open` CLI command. The CLI shortcut is:

```bash
pinchtab instance navigate inst_ea2e747f https://pinchtab.com
```

That command opens a tab for the instance already on the URL, in one `tabs/open` call.

## List Tabs For One Instance

```bash
curl http://localhost:9867/instances/inst_ea2e747f/tabs
```

## List All Tabs Across Running Instances

```bash
curl http://localhost:9867/instances/tabs
```

This is the fleet-wide tab listing endpoint. It is different from `GET /tabs`, which is shorthand or bridge scoped.

Both routes return a bare JSON array of `{"id","instanceId","url","title"}` objects, cached per
instance; add `?fresh=1` to refetch. `GET /instances/{id}/tabs` answers `503` when the instance is not running.

## List Metrics Across Instances

```bash
curl http://localhost:9867/instances/metrics
```

`GET /instances/{id}/metrics` returns one instance's own request counters; the bare
`GET /metrics` on the server answers with the server's counters (see [Metrics](./metrics.md)).

## Attach An Existing Browser (CDP)

```bash
curl -X POST http://localhost:9867/instances/attach \
  -H "Content-Type: application/json" \
  -d '{
    "name":"shared-chrome",
    "cdpUrl":"ws://127.0.0.1:9222/devtools/browser/...",
    "provider":"chrome",
    "browser":"chrome-local"
  }'
```

What this does:

- spawns a child `pinchtab bridge --cdp-attach ...` process that wraps the
  external CDP browser via `chromedp.NewRemoteAllocator`
- registers the instance with `attached=true`, `attachType="cdp-bridge"`,
  and the routable `url` set to the **HTTP bridge URL** (not the raw `ws://`
  CDP URL)
- preserves the original `cdpUrl` as metadata in the API response
- routes `/tabs`, `/snapshot`, `/action`, `/screenshot`, etc. through the
  child bridge

Accepted `cdpUrl` shapes:

- browser-level WebSocket URL: `ws://host:port/devtools/browser/<id>`
- HTTP DevTools origin: `http://host:port` (resolved through `/json/version`)
- HTTP `/json/version` URL

Page-level URLs (`/devtools/page/...`) are rejected.

`browser` is optional and accepts a provider name (`chrome`, `cloak`) or a
configured target name from `browser.targets`. When `browser.targets` is
configured, an omitted value attaches to the configured default target and the
target's browser is used. If `provider` is also present, it must agree with
the `browser` value. Without `browser.targets`,
`provider` is `chrome` (default) or `cloak`; the cloak browser disables
PinchTab JS overlays on the assumption that the external browser owns native
fingerprint behavior.

Notes:

- there is no CLI attach command
- attach is allowed only when enabled in config under `security.attach`
- `security.attach.allowHosts` must allow the `cdpUrl` host
- if you pass an HTTP DevTools origin, `security.attach.allowSchemes` must
  include `http` or `https`
- stopping the attached instance shuts down the child PinchTab bridge but
  leaves the external browser process running
- `allowHosts: ["*"]` is a documented, non-default, security-reducing override

## Attach An Existing Bridge

```bash
curl -X POST http://localhost:9867/instances/attach-bridge \
  -H "Content-Type: application/json" \
  -d '{
    "name":"shared-bridge",
    "baseUrl":"http://10.0.12.24:9868",
    "token":"bridge-secret-token",
    "browser":"chrome-local"
  }'
```

Notes:

- `baseUrl` must be a bare bridge origin; do not include credentials, query strings, fragments, or a path
- `browser` is optional and accepts a provider or target name; with `browser.targets` configured, an omitted value attaches to the default target
- the orchestrator performs a health check before registering it
- `security.attach.allowHosts` must allow the bridge host
- `allowHosts: ["*"]` is a documented, non-default, security-reducing override. It disables host allowlisting entirely and allows any reachable bridge host with an allowed scheme. Use it only on isolated, operator-controlled networks.
