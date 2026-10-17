# Instance Charts

This page captures the current instance model in PinchTab.

It is intentionally limited to the instance types and relationships that exist now in the codebase.

## Chart 1: Current Instance Types

```mermaid
flowchart TD
    I["Instance"] --> M["Managed"]
    I --> A["Attached"]

    M --> MB["Bridge-backed"]
    A --> EX["External Chrome via child bridge (cdp-bridge)"]
    A --> EB["External bridge registration (bridge)"]
```

Current meaning:

- **managed** means PinchTab launches and owns the runtime lifecycle
- **attached** means PinchTab fronts something it did not launch: an already running external Chrome (`POST /instances/attach`, through a child `pinchtab bridge --cdp-attach`) or an already running bridge server (`POST /instances/attach-bridge`)
- **bridge-backed** means the server reaches the browser through a `pinchtab bridge` runtime; that holds for every instance type

## Chart 2: Managed Instance Shape

```mermaid
flowchart LR
    S["PinchTab Server"] --> O["Orchestrator"]
    O --> B["pinchtab bridge child"]
    B --> C["Chrome"]
    C --> T["Tabs"]
    O --> P["Profile directory"]
```

For managed instances today:

- the orchestrator launches a bridge child process
- the bridge owns one browser runtime
- tabs live inside that runtime
- browser state is tied to the associated profile directory

## Chart 3: Attached Instance Shape

```mermaid
flowchart LR
    S["PinchTab Server"] --> O["Orchestrator"]
    O --> B["pinchtab bridge --cdp-attach child"]
    B -. "CDP URL" .-> E["External Chrome"]
    E --> T["Tabs"]
    O -. "attach-bridge" .-> XB["External pinchtab bridge"]
```

For attached instances today:

- PinchTab does not launch the browser
- for a CDP attach, PinchTab launches and stops a child bridge that connects to the external browser; for `attach-bridge` it only registers the remote bridge's URL
- PinchTab stores registration metadata in the instance registry
- ownership of the external browser process stays outside PinchTab
- both attach types expose a bridge URL, so tab-scoped routes proxy to them the same way as to managed instances

## Chart 4: Ownership Model

```mermaid
flowchart TD
    I["Instance"] --> L["Lifecycle owner"]

    L --> P["PinchTab"]
    L --> X["External process owner"]

    P --> M["Managed instance"]
    X --> A["Attached instance"]
```

This is the key distinction:

- managed instances are lifecycle-owned by PinchTab
- attached instances are tracked by PinchTab but not process-owned by PinchTab

## Chart 5: Routing Relationship

```mermaid
flowchart TD
    I["Instance"] --> T["Tabs"]
    T --> R["Tab-scoped routes"]
    R --> O["Owning instance resolution"]
```

The important runtime rule is:

- tabs belong to one instance
- tab-scoped server routes are resolved to the owning instance before proxying, for managed and attached instances alike

## Current Instance Fields

The main instance fields surfaced by the current API are:

- `id`
- `profileId`
- `profileName`
- `port`
- `url`
- `mode` (`headless` or `headed`)
- `headless`
- `status`
- `startTime`
- `error`
- `attached`
- `attachType` (`cdp-bridge` or `bridge`, for attached instances)
- `cdpUrl`
- `securityPolicy`
- `browser`
- `responsiveness`
- `crashes`, `fallbackFrom`, `fallbackReason` (when present)

Useful interpretation:

- `attached: false` usually means a managed bridge-backed instance
- `attached: true` means an externally registered instance
- `port` is relevant for managed and CDP-attached instances (the child bridge's port)
- `cdpUrl` is relevant for CDP-attached instances
