# PinchTab Environment Variables

This reference is intentionally narrow.

For agent workflows, most runtime behavior should be configured through `config.json` or the `pinchtab config` commands, not environment variables.

## Agent-relevant variables

| Var | Typical use | Notes |
|---|---|---|
| `PINCHTAB_TOKEN` | Authenticate CLI or MCP requests to a protected server | Sent as `Authorization: Bearer ...` |
| `PINCHTAB_CONFIG` | Override the config file path | Prefer this over ad hoc env overrides when automating |
| `PINCHTAB_SESSION` | Agent session token | Sent as `Authorization: Session ...`; takes precedence over `PINCHTAB_TOKEN` |
| `PINCHTAB_SERVER` | Same as `--server` | Targets that server instead of auto-starting a local one |
| `PINCHTAB_AGENT_ID` | Same as `--agent-id` | Scopes the current tab per agent |
| `PINCHTAB_HINTS` | `off` silences CLI advisory hints | |

## Targeting remote servers

Use the `--server` CLI flag instead of environment variables, and pair it with that host's credential — the CLI never sends the local config's `server.token` to a remote host, so without `PINCHTAB_TOKEN` (or `PINCHTAB_SESSION`) it exits with an error:

```bash
PINCHTAB_TOKEN=<that-host-token> pinchtab --server http://192.168.1.50:9867 snap
PINCHTAB_TOKEN=<that-host-token> pinchtab --server https://pinchtab.com snap
```

There is deliberately no `--token` flag: a credential in argv is visible to every user on the host via the process list and lands in shell history, so the env-var form is the supported pairing.

## What is intentionally not listed

- Browser tuning should generally live in `config.json`, not in ad hoc env vars.
- Internal process wiring and inherited env passthrough are implementation details, not part of the skill contract.

## Recommended default

For most agent tasks, the only variable you need is:

```bash
PINCHTAB_TOKEN=...
```

For multi-step flows on the same tab, run `pinchtab nav URL` once and then use
unscoped commands. Anonymous CLI calls remember the current tab in a shared
local state file. Identified callers use server-side current-tab state instead:
agent sessions scope the current tab by session, and `--agent-id` /
`PINCHTAB_AGENT_ID` scope it by agent ID when no session is present. Use
`--tab <id>` only when you need to target a specific tab explicitly.

Or use agent sessions for per-agent identity and revocability:

```bash
PINCHTAB_SESSION=ses_...
```

When `PINCHTAB_SESSION` is set, the CLI uses `Authorization: Session <token>` instead of bearer auth. The session maps to a specific agentId server-side and can be revoked independently.

A session token cannot use admin routes (`/instances*`, `/profiles*`, `/sessions*`, config): start instances and create profiles with `PINCHTAB_TOKEN` before exporting `PINCHTAB_SESSION`. Sessions exist only on `pinchtab server`, not on a standalone `pinchtab bridge`; there, use `--agent-id` / `PINCHTAB_AGENT_ID` for a per-agent current tab.

Everything else should be handled through config, profiles, instances, and the `--server` flag.
