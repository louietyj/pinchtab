# Orchestration

This page describes the current orchestration layer in PinchTab: how the server launches, tracks, routes to, and stops browser instances.

## Scope

The orchestrator is part of server mode. It is responsible for:

- launching managed instances as child `pinchtab bridge` processes
- attaching externally managed Chrome instances when attach policy allows it
- tracking instance status and metadata
- routing tab-scoped requests to the owning managed instance
- stopping managed instances and cleaning up registry state

It does not execute browser actions directly. That work happens inside the bridge runtime.

## Current Runtime Shape

```mermaid
flowchart TD
    S["PinchTab Server"] --> O["Orchestrator"]

    O --> M1["Managed Instance"]
    O --> M2["Managed Instance"]
    O -.->|attach| A1["Attached External Instance"]

    M1 --> B1["pinchtab bridge child"]
    M2 --> B2["pinchtab bridge child"]

    B1 --> C1["Chrome"]
    B2 --> C2["Chrome"]
```

## Launch Flow

For a managed instance, the orchestration flow is:

```mermaid
flowchart LR
    R["Start Request"] --> V["Validate profile + port"]
    V --> W["Write child config"]
    W --> P["Spawn pinchtab bridge"]
    P --> H["Poll /health on configured bind or loopback"]
    H --> S{"Healthy before timeout?"}
    S -->|Yes| OK["Mark running"]
    S -->|No| ER["Mark error"]
```

What the code does today:

- validates the profile name before launch
- allocates a port when one is not supplied
- prevents more than one active managed instance per profile
- prevents reusing a port already in use
- writes a child config file under the profile state directory
- launches `pinchtab bridge`
- polls `/health` on the configured child bind first when present, then falls
  back to `127.0.0.1`, `::1`, and `localhost`
- moves the instance from `starting` to `running` or `error`

## Attach Flow

Attach is a separate path for an already running browser or bridge.

```mermaid
flowchart LR
    R["POST /instances/attach"] --> P["Validate attach policy"]
    P --> B["Spawn pinchtab bridge --cdp-attach child"]
    B --> H{"Child healthy before timeout?"}
    H -->|Yes| L["Registry: attached, attachType cdp-bridge"]
    H -->|No| X["Stop child, return error"]
    RB["POST /instances/attach-bridge"] --> PB["Validate attach policy"]
    PB --> LB["Health-check + registry: attached, attachType bridge"]
```

Current attach behavior:

- requires `security.attach.enabled`
- validates the URL against `security.attach.allowSchemes`
- validates the host against `security.attach.allowHosts`
- `POST /instances/attach` (CDP URL) launches a child `pinchtab bridge --cdp-attach <url>` on an allocated port, waits for its `/health`, and registers it as `attached: true`, `attachType: "cdp-bridge"`
- `POST /instances/attach-bridge` registers an already running bridge server as `attachType: "bridge"` (upserted by name when the token matches)
- never starts or kills the external Chrome process itself

## Routing Model

The orchestrator is also the routing layer for multi-instance server mode.

```mermaid
flowchart LR
    R["Tab-scoped request"] --> C["Locator (cache, then /tabs scan)"]
    C -->|found| P["Proxy to owning instance URL"]
    C -->|miss| F["Scan running instances: /tabs?includeTransient=1"]
    F -->|found| P
    F -->|not found| S{"Exactly one running instance?"}
    S -->|Yes| P
    S -->|No| N["404 tab not found"]
```

Today, tab routing (`routeByTabOwner` in `internal/orchestrator/route.go`) works like this:

- for routes such as `/tabs/{id}/navigate` and `/tabs/{id}/action`, the server resolves which instance owns the tab
- it first asks the instance locator (`internal/instance`), which checks its tab→instance cache and on a miss scans each running instance's user-facing `/tabs` list
- if that misses, the orchestrator scans running instances with `/tabs?includeTransient=1`, the unfiltered list that also contains tabs the UI list hides (for example `about:blank`, `file://`, or the instance's own port), and records the owner in the locator
- if no owner is found and exactly one instance is running, the request falls through to it; otherwise it returns 404
- a `browser` that conflicts with the owner's browser is rejected before proxying
- after resolution, it proxies the request to the owning instance's URL

Requests without a tab id go to the instance bound to the caller's session or agent identity when there is one, and otherwise to the active strategy's fallback, which targets the earliest-started running instance matching the requested or default browser (the `simple` strategy launches one when none is running). With no browser requested, that is the same instance `/health` reports as `defaultInstance` (`DefaultInstance()`).

This keeps the public server API stable while the bridge instances remain isolated. Attached instances (both attach types) expose a bridge HTTP URL, so they use the same proxy path.

## Stop Flow

Stopping a managed instance is a server-owned lifecycle operation.

```mermaid
flowchart LR
    R["Stop Request"] --> S["Mark stopping"]
    S --> G["POST /shutdown to instance"]
    G --> W{"Exited?"}
    W -->|No| T["SIGTERM"]
    T --> K{"Exited?"}
    K -->|No| X["SIGKILL"]
    W -->|Yes| D["Remove from registry"]
    K -->|Yes| D
    X --> D
```

Current stop behavior:

- marks the instance as `stopping`
- tries graceful shutdown through the instance HTTP API
- falls back to process-group termination when needed
- releases the allocated port
- removes the instance from the registry and locator cache

For CDP-attached instances, the child bridge is stopped the same way; the external Chrome is left running. For `attach-bridge` instances there is no child process: the orchestrator sends `POST /shutdown` to the registered bridge, waits for its endpoint to go away, and then removes the registration.

## Instance States

The main statuses surfaced today are:

- `starting`
- `running`
- `stopping`
- `stopped`
- `error`

The orchestrator also emits lifecycle events such as:

- `instance.launched`
- `instance.started`
- `instance.stopped`
- `instance.error`
- `instance.attached`
- `instance.reattached`

`Orchestrator.List()` returns instances ordered by start time (ties broken by ID), so listings are stable across calls.

## Relationship To Other Layers

- **Strategy layer** decides how shorthand requests are exposed or routed in server mode
- **Scheduler** is optional and sits above the same routed execution path
- **Bridge** owns browser state, tab state, and CDP execution
- **Profiles** provide persistent browser data on disk
