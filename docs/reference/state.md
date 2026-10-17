# State

`pinchtab state` shows the current full browser state for a tab, and also manages saved browser state on disk.

There are three related state views:

- **full browser state** via `pinchtab state` or `GET /state`
- **live tab state** via `GET /tabs/{id}/state`
- **current-origin storage** via `pinchtab storage ...`

Full browser state includes:

- cookies
- current-origin `localStorage`
- current-origin `sessionStorage`
- optional metadata

All full/saved state operations require `security.allowStateExport=true`; when it is off they answer `403 state_export_disabled`.

## Commands

```bash
pinchtab state [--tab <id>]
pinchtab state list
pinchtab state save [--name <name>] [--encrypt] [--tab <id>]
pinchtab state load --name <name-or-prefix> [--tab <id>]
pinchtab state show --name <name>
pinchtab state delete --name <name>
pinchtab state clean [--older-than <hours>]
```

`save`, `load`, `show` and `delete` also accept the name as a positional
argument (`pinchtab state load work-login`); `--name` is the same value.

## Show Current Browser State

```bash
pinchtab state
pinchtab state --tab <tabId>
```

Returns the current full browser state for the active tab or the specified tab:

- cookies
- current-origin storage
- tab information such as `tabId`, URL, and title
- metadata such as origin and user agent

Use this when you need the richer gated state view. For the lightweight operational/readiness view, use `GET /tabs/{id}/state`.

## List Saved States

```bash
pinchtab state list
```

Lists saved state files in the configured state directory.

## Save Current Browser State

```bash
pinchtab state save
pinchtab state save --name work-login
pinchtab state save --name checkout --tab <tabId>
pinchtab state save --name work-login --encrypt
```

Notes:

- omitting `--name` lets PinchTab auto-generate one
- `--tab <id>` captures state from a specific tab instead of the active/current one
- `--encrypt` requires `security.stateEncryptionKey` (or the `PINCHTAB_STATE_KEY` environment variable, which wins); without one the save is `400`

## Load A Saved State

```bash
pinchtab state load --name work-login
pinchtab state load --name work-log    # prefix match, newest match wins
pinchtab state load --name checkout --tab <tabId>
```

Notes:

- `--name` accepts either an exact name or a prefix; an exact match wins
- prefix matching resolves to the most recent matching saved state
- loading restores the saved cookies, then writes the saved `localStorage` and
  `sessionStorage` items into the target tab's current page, so navigate the tab
  to the saved origin first
- the response reports `cookiesRestored`, `storageItemsRestored` and `origins`

## Show Saved State Details

```bash
pinchtab state show --name work-login
```

Displays the full saved browser state record, including stored metadata and origin storage payloads. Unlike `load`, `show` needs the exact name.

## Delete A Saved State

```bash
pinchtab state delete --name work-login
```

Removes the named state file from disk.

## Clean Old Saved State Files

```bash
pinchtab state clean
pinchtab state clean --older-than 72
```

Removes saved state files older than the given number of hours. Default is `24` (a zero or negative value also means `24`).

## HTTP API

```text
GET    /state
GET    /state/list
GET    /state/show
POST   /state/save
POST   /state/load
DELETE /state
POST   /state/clean
```

| Route | Input |
| --- | --- |
| `GET /state` | `?tabId=` (optional) |
| `GET /state/show`, `DELETE /state` | `?name=` (required, else `400`) |
| `POST /state/save` | `{"name", "encrypt", "tabId", "metadata"}`, fields optional but a JSON body (at least `{}`) is required; returns `name`, `path`, `cookies` (count), `origins`, `encrypted` |
| `POST /state/load` | `{"name", "tabId"}`, `name` required |
| `POST /state/clean` | `{"olderThanHours"}` (JSON body required); returns `removed`, `olderThanHours`, `sessionsDir` |

`GET /state/list` returns `{states, count}`; saved files live under
`<stateDir>/sessions/`.

See [Endpoints](../endpoints.md) for the short route index. Use `GET /state?tabId=<id>` for the full gated browser-state view and `GET /tabs/{id}/state` for live readiness/blocking data.

## Related

- [Tabs](./tabs.md) for live tab work and `--tab <id>`
- [Sessions](./sessions.md) for agent/session auth
- [Config](./config.md) for security flags and storage directories
