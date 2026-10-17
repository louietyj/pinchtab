# Architecture

PinchTab is a local HTTP control plane for Chrome aimed at agent-driven browser automation. Callers talk to PinchTab over HTTP and JSON; PinchTab translates those requests into browser work through Chrome DevTools Protocol.

## Runtime Roles

PinchTab has two runtime roles:

- **Server**: `pinchtab` or `pinchtab server`
- **Bridge**: `pinchtab bridge`

Today, the main production shape is:

- the **server** manages profiles, instances, routing, and the dashboard
- each managed instance is a separate **bridge-backed** browser runtime
- the bridge owns one browser context and serves the single-instance browser API

PinchTab also supports an advanced attach path:

- the server can front an externally managed Chrome instance through `POST /instances/attach` (it launches a child `pinchtab bridge --cdp-attach` against the CDP URL), or register an already running bridge through `POST /instances/attach-bridge`
- attach is policy-gated by `security.attach.enabled`, `security.attach.allowHosts`, and `security.attach.allowSchemes`

The current managed implementation is bridge-backed. Any direct-CDP-only managed model is architectural discussion elsewhere, not the default runtime path in this codebase.

Related architecture specs:

- [Browser Abstraction](./browser-abstraction.md): the multi-browser target model
  for selecting Chrome, CloakBrowser, and future providers per request (see the
  Target Architecture section).
- [Geo Provider](./geo-provider.md): proxy egress geo resolution, including the
  proposed contract for a future HTTP-backed provider.

## System Overview

```mermaid
flowchart TD
    A["Agent / CLI / HTTP Client"] --> S["PinchTab Server"]

    S --> D["Dashboard + Config + Profiles API"]
    S --> O["Orchestrator + Strategy Layer"]
    S --> Q["Optional Scheduler"]

    O --> M1["Managed Instance"]
    O --> M2["Managed Instance"]

    M1 --> B1["pinchtab bridge"]
    M2 --> B2["pinchtab bridge"]

    B1 --> C1["Chrome"]
    B2 --> C2["Chrome"]

    C1 --> T1["Tabs"]
    C2 --> T2["Tabs"]

    S -. "advanced attach path" .-> E["Registered External Chrome"]
```

## Request Flow

For the normal multi-instance server path, the flow is:

```mermaid
flowchart LR
    R["HTTP Request"] --> M["Auth + Middleware"]
    M --> X["Routing / Instance Resolution"]
    X --> B["Bridge Handler"]
    B --> P["Handler Policy Checks"]
    P --> C["Chrome via CDP"]
    C --> O["JSON / Text / PDF / Image Response"]
```

Important details:

- auth and common middleware run at the HTTP layer
- attach policy is enforced on the attach routes in the server
- tab-scoped routes are resolved to the owning instance before execution
- browser-facing checks (open-dialog guard, domain policy, and IDPI when enabled) run in the bridge handlers, before any CDP work
- the bridge runtime performs the actual CDP work

In bridge-only mode, the orchestrator and multi-instance routing layers are skipped, but the same browser handler model still applies.

## Current Architecture

The current implementation centers on these pieces:

- **Profiles**: persistent browser state stored on disk
- **Instances**: running browser runtimes associated with profiles or external CDP URLs
- **Tabs**: the main execution surface for navigation, extraction, and actions
- **Orchestrator**: launches, tracks, stops, and proxies managed instances
- **Bridge**: owns the browser context, tab registry, ref cache, and action execution

The main instance types in practice are:

- **managed bridge-backed instances** launched by the server
- **attached external instances** registered through the attach API

## Security And Policy Layer

PinchTab's protection logic lives in the HTTP handler layer, not in the caller and not in Chrome itself.

When `security.idpi` is enabled, the current implementation can:

- block or warn on navigation targets using domain policy
- scan `/text` output for common prompt-injection patterns
- scan `/snapshot` content for the same class of patterns
- wrap `/text` output in `<untrusted_web_content>` when configured

Architecturally, this keeps policy separate from routing and execution:

```text
request -> middleware -> routing -> handler policy -> execution -> response
```

## Design Principles

- **HTTP for callers**: agents and tools talk to PinchTab over HTTP, not raw CDP
- **A11y-first interaction**: snapshots and refs are the primary structured interface
- **Instance isolation**: managed instances run separately and keep isolated browser state
- **Tab-scoped execution**: once a tab is known, actions route to that tab's owning runtime
- **Optional coordination layers**: strategy routing and the scheduler sit above the same browser execution surface

## Code Map

The most important packages for the current architecture are:

- `cmd/pinchtab`: process startup modes and CLI entrypoints
- `internal/orchestrator`: instance lifecycle, attach, and tab-to-instance proxying
- `internal/instance`: tab-to-instance locator cache and instance allocation used by the orchestrator
- `internal/bridge`: browser runtime, tab state, and CDP execution
- `internal/browsers`: browser provider registry (`chrome`, `cloak`, `ghost-chrome`)
- `internal/handlers`: single-instance HTTP handlers
- `internal/profiles`: persistent profile management
- `internal/strategy`: server-side routing behavior for shorthand requests
- `internal/scheduler`: optional queued task dispatch
- `internal/config`: runtime and file config loading
