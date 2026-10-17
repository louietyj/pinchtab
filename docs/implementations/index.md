# Implementations

This section covers implementation-focused documents: how specific subsystems work in practice, what tradeoffs they make, and how the current code is structured.

Use these pages when you want lower-level detail than the architecture overview, but do not need full API reference material.

- [Lite Engine (historical)](./lite-engine.md) — the removed `chrome`/`lite`/`auto` engine router; its Gost-DOM path survives as the static fetch used by the `ghost-chrome` provider (`internal/browsers/ghostchrome/staticfetch`) — see [terminology](../architecture/terminology.md)
- [Managed Bridge vs Managed Direct CDP](./managed-bridge-vs-managed-direct-cdp.md)
- [Chrome Profile Lock Recovery](./chrome-profile-lock-recovery.md)
- [Chrome Files](./chrome-files.md)
- [Parallel Tab Execution (design write-up)](./parallel-tab-execution.md)
- [Docker Local Testing](./docker-local-testing.md)
