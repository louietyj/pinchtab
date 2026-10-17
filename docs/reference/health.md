# Health

Check server status and availability.

`/health` is deliberately **not** the endpoint to compare across modes: in server
mode it is the orchestrator's own envelope, and an orchestrator has facts
(`instances`, `profiles`, `defaultInstance`) a bridge does not. Failure and crash
telemetry is served in the same shape by both modes on
[`/metrics`](./metrics.md) — use that when comparing a bridge repro against a
server-mode problem.

## Bridge Mode

```bash
curl http://localhost:9867/health
# Response: {"status":"ok","tabs":1}

# CLI Alternative (human-readable by default)
pinchtab health
# Output: ok (followed by next-step hints)

pinchtab health --json              # Full JSON response
```

`tabs` counts every browser target, including ones `GET /tabs` hides (such as `about:blank`).
Bridge-mode health also reports `version`, and may include:

- `crashLogs`
- `failures`
- `crashes`

In error cases it returns `503` with `status: "error"`, a `reason` and an error `code`
(`bridge_unavailable`, `browser_init_failed`, `list_targets_failed`). While the browser is
restarting it returns `503` `browser_draining` with `status: "draining"`, `retryAfterSeconds`
and a `Retry-After` header.

## Server Mode (Dashboard)

In full server mode, `/health` returns the dashboard health envelope:

```bash
curl http://localhost:9867/health
# Response
{
  "status": "ok",
  "mode": "dashboard",
  "version": "0.8.0",
  "uptime": 12345,
  "authRequired": true,
  "profiles": 1,
  "temporaryProfiles": 0,
  "quarantinedProfiles": 0,
  "instances": 1,
  "defaultInstance": {
    "id": "inst_abc12345",
    "status": "running",
    "responsiveness": "responsive"
  },
  "agents": 0,
  "restartRequired": false
}
```

| Field | Description |
| --- | --- |
| `status` | `ok` when server is healthy; `degraded` while at least one instance is unresponsive |
| `mode` | `dashboard` in server mode |
| `version` | PinchTab version |
| `uptime` | Milliseconds since server start |
| `authRequired` | `true` when a server token is configured |
| `profiles` | Number of profiles `GET /profiles` lists (neither temporary nor quarantined) |
| `temporaryProfiles` | Number of temporary auto-generated instance profiles |
| `quarantinedProfiles` | Number of quarantined profiles |
| `instances` | Number of managed instances |
| `defaultInstance` | The instance shorthand (non-`/instances`) routes use: `id`, `status`, `responsiveness`. See note below |
| `agents` | Connected agent count |
| `restartRequired` | `true` when file-based config changes need restart |
| `restartReasons` | Restart reason list when required |
| `security` | Only for requests authenticated by bearer token or dashboard cookie: `level`, `bind`, `allowedDomains`, `idpiEnabled`, `enabledSensitiveEndpoints`, `guardsDown` |
| `crashes` | Present once any instance's browser has crashed: `total` and `recent`, the same block bridge `/health` carries, each event naming its `instanceId` |
| `unresponsiveInstances` | Present while any instance answers its own `/health` but not its browser routes: the ids of those instances |

Notes:

- `defaultInstance` is present when at least one instance exists. It is the earliest-started running instance of the
  default browser target; with none, it falls back to the earliest-started instance of any status
- use `defaultInstance.status == "running"` when you want to confirm Chrome is ready
- strategies such as `always-on` can create an instance automatically at startup
- `status` does not degrade on a browser crash: the instance is relaunched and is serving again. The crash is history, so it rides beside `status` as `crashes`; a crashed-then-relaunched instance differs from one that never crashed by that key alone. Every tab the dead browser held is gone, and a call to one answers `404` with code `browser_crashed` and a `hint` saying so
- `status` does degrade to `degraded` while an instance is unresponsive, because that condition is current: every `/health` starts a probe of each running instance's `/tabs` under a short budget beside its crash fetch, or joins the one already in flight, and waits for it at most 250ms; a probe that takes longer finishes in the background, so that `/health` reports the last recorded result and a later one reports the new result. An instance whose `/health` answers while `/tabs` exceeds the budget is named in `unresponsiveInstances`. Its own `status` stays `running` and nothing restarts it; the field is a report, not a remedy. See `responsiveness` on `GET /instances`

## Related Pages

- [Tabs](./tabs.md)
- [Navigate](./navigate.md)
- [Strategies](./strategies.md)
