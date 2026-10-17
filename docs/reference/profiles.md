# Profiles

Profiles are browser user data directories. They hold cookies, local storage, history, and other durable browser state.

In PinchTab:

- profiles exist even when no instance is running
- one profile can have at most one active managed instance at a time
- profile IDs and names are both useful, but some endpoints require the profile ID specifically

## List Profiles

```bash
curl http://localhost:9867/profiles
# Response: JSON array (see below)

# CLI Alternative (human-readable by default)
pinchtab profiles
# Output (tab-separated): prof_278be873  work

pinchtab profiles --json              # Full JSON response
```

`pinchtab profiles` is the simplest way to see available profiles from the CLI. Quarantined
profiles are listed separately after the live ones, with their size.

Response shape (every field is always present; the same object is returned by `GET /profiles/{id}`):

```json
[
  {
    "id": "prof_278be873",
    "name": "work",
    "path": "/path/to/profiles/work",
    "pathExists": true,
    "created": "2026-02-27T20:37:13.599055326Z",
    "lastUsed": "2026-03-01T09:12:44Z",
    "diskUsage": 534952089,
    "sizeMB": 510.17,
    "running": false,
    "quarantined": false,
    "temporary": false,
    "source": "created",
    "chromeProfileName": "",
    "accountEmail": "",
    "accountName": "",
    "hasAccount": false,
    "useWhen": "Use for work accounts",
    "description": ""
  }
]
```

Notes:

- `GET /profiles` excludes temporary auto-generated instance profiles by default
- use `GET /profiles?all=true` to include temporary profiles (`"temporary": true`)

## Get One Profile

```bash
curl http://localhost:9867/profiles/prof_278be873
# Response
{
  "id": "prof_278be873",
  "name": "work",
  "path": "/path/to/profiles/work",
  "pathExists": true,
  "created": "2026-02-27T20:37:13.599055326Z",
  "lastUsed": "2026-03-01T09:12:44Z",
  "diskUsage": 534952089,
  "sizeMB": 510.17,
  "running": false,
  "quarantined": false,
  "temporary": false,
  "source": "created",
  "chromeProfileName": "Your Chrome",
  "accountEmail": "admin@pinchtab.com",
  "accountName": "Luigi",
  "hasAccount": true,
  "useWhen": "Use for work accounts",
  "description": ""
}
```

`GET /profiles/{id}` accepts either the profile ID or the profile name.

## Create A Profile

```bash
curl -X POST http://localhost:9867/profiles \
  -H "Content-Type: application/json" \
  -d '{"name":"scraping-profile","description":"Used for scraping","useWhen":"Use for ecommerce scraping"}'
# Response
{
  "status": "created",
  "id": "prof_0f32ae81",
  "name": "scraping-profile"
}
```

Notes:

- `name` is required; `description` and `useWhen` are optional
- both `POST /profiles` and `POST /profiles/create` work for creating profiles
- the CLI form is `pinchtab profiles create <name>`, which prints the new `id` and `name`

## Update A Profile

```bash
curl -X PATCH http://localhost:9867/profiles/prof_278be873 \
  -H "Content-Type: application/json" \
  -d '{"description":"Updated description","useWhen":"Updated usage note"}'
# Response
{
  "status": "updated",
  "id": "prof_278be873",
  "name": "work"
}
```

You can also rename the profile:

```bash
curl -X PATCH http://localhost:9867/profiles/prof_278be873 \
  -H "Content-Type: application/json" \
  -d '{"name":"work-renamed"}'
```

Important:

- `PATCH /profiles/{id}` requires the profile ID
- using the profile name in that path returns an error
- a rename changes the generated profile ID because IDs are derived from the name

`PATCH /profiles/meta` updates `description` and/or `useWhen` for the profile named by `name`
in the body, and answers `{"status":"updated","name":"..."}`.

## Delete A Profile

```bash
curl -X DELETE http://localhost:9867/profiles/prof_278be873
# Response
{
  "status": "deleted",
  "id": "prof_278be873",
  "name": "work"
}
```

`DELETE /profiles/{id}` also requires the profile ID. Add `?force=true` to delete a profile
that still has an instance; the response then names that instance as `orphanedInstance`.

## Start Or Stop By Profile

Start the active instance for a profile:

```bash
curl -X POST http://localhost:9867/profiles/prof_278be873/start \
  -H "Content-Type: application/json" \
  -d '{"headless":true}'
# Response
{
  "id": "inst_ea2e747f",
  "profileId": "prof_278be873",
  "profileName": "work",
  "port": "9868",
  "mode": "headless",
  "headless": true,
  "status": "starting"
}
```

Stop the active instance for a profile:

```bash
curl -X POST http://localhost:9867/profiles/prof_278be873/stop
# Response
{
  "status": "stopped",
  "id": "prof_278be873",
  "name": "work"
}
```

For these orchestrator routes, the path can be a profile ID or profile name. Start answers `201` with the
instance object (see [Instances](./instances.md)), which includes both `mode` and `headless`. Its body takes
`headless` (default `false`, so omitting it starts a headed browser), `port`, `securityPolicy`, `browser`
and `fallbackTargets`.

## Check Whether A Profile Has A Running Instance

```bash
curl http://localhost:9867/profiles/prof_278be873/instance
# Response
{
  "name": "work",
  "exists": true,
  "running": true,
  "status": "running",
  "port": "9868",
  "id": "inst_ea2e747f"
}
```

`status` is `running` or `starting` when an instance exists (`id` is then set), `stopped` when it
does not, and `missing` (with `exists: false` and a `message`) when the profile does not exist.
The route always answers `200`.

## Additional Profile Operations

### Reset A Profile

```bash
curl -X POST http://localhost:9867/profiles/prof_278be873/reset
```

This route requires the profile ID and answers `{"status":"reset","id":"...","name":"..."}`.

### Import A Profile

```bash
curl -X POST http://localhost:9867/profiles/import \
  -H "Content-Type: application/json" \
  -d '{"name":"imported-profile","sourcePath":"/path/to/existing/profile"}'
```

`name` and `sourcePath` are required; `description` and `useWhen` are optional. The response is
`{"status":"imported","name":"..."}`.

### Prune Quarantined Profiles

```bash
curl -X POST http://localhost:9867/profiles/prune
curl -X POST http://localhost:9867/profiles/prune \
  -H "Content-Type: application/json" \
  -d '{"confirm":true}'
# CLI Alternative
pinchtab profiles prune              # list what would be removed
pinchtab profiles prune --confirm    # remove it
```

Without `confirm` (body or `?confirm=true`) nothing is deleted and the response lists what would be.
`profile` (body or query) limits it to one quarantined directory. The response is
`{"removed":<bool>,"count":N,"totalBytes":N,"profiles":[{"name","path","bytes"}]}`.

### Get Logs

```bash
curl http://localhost:9867/profiles/prof_278be873/logs
curl 'http://localhost:9867/profiles/work/logs?limit=50'
```

`logs` accepts either the profile ID or the profile name. Results are derived from the activity store for that profile.

### Get Analytics

```bash
curl http://localhost:9867/profiles/prof_278be873/analytics
curl http://localhost:9867/profiles/work/analytics
```

`analytics` also accepts either the profile ID or the profile name. It is computed from the same activity data used by `/api/activity`.

## Related Pages

- [Instances](./instances.md)
- [Tabs](./tabs.md)
- [Config](./config.md)
