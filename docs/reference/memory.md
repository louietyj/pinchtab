# Memory

PinchTab can answer "is this page leaking?" without a human at DevTools: read the
tab's heap usage, write V8 heap snapshots to server-side files, summarize one, and
compare two to see which constructors grew.

`GET /memory` is always available. Everything that touches a heap snapshot needs
`security.allowMemory` (default off; code `memory_disabled` when off), because a
heap snapshot holds every string on the page, tokens included.

## Endpoints

| Method | Path | Capability | Purpose |
| --- | --- | --- | --- |
| `GET` | `/memory`, `/tabs/{id}/memory` | none | Heap usage and DOM counters |
| `POST` | `/memory/snapshot`, `/tabs/{id}/memory/snapshot` | `allowMemory` | Take a heap snapshot to a file |
| `GET` | `/memory/snapshot/{snapshotId}/summary` | `allowMemory` | Top constructors and duplicate strings of one snapshot |
| `GET` | `/memory/compare` | `allowMemory` | Constructor growth between two snapshots |

## Usage

`GET /memory?tabId=<id>&gc=true` returns `tabId`, `usedJSHeapSize`, `totalJSHeapSize`,
`jsHeapSizeLimit`, `documents`, `nodes`, `listeners`, `frames` and `gc`. `gc=true` runs a
garbage collection first so two reads compare live memory only; a `gc` value that is
not a boolean is `400 bad_gc`.

```bash
pinchtab memory --gc            # --tab <id>, --json
```

MCP `pinchtab_memory` takes `tabId`, `gc` and `browser`.

## Snapshot

`POST /memory/snapshot` with an optional `{"tabId": "..."}` streams the snapshot to
`<stateDir>/heapsnapshots/<id>.heapsnapshot` and returns
`{id, path, bytes, nodeCount, durationMs, tabId}`. The file loads in the Chrome
DevTools Memory panel. A snapshot larger than `security.memorySnapshotMaxBytes`
(default 512 MB, capped at 4 GB) is discarded and answered
`413 memory_snapshot_too_large` (details carry `maxBytes`).

```bash
pinchtab memory snapshot                          # prints the id and path
pinchtab memory snapshot --out app.heapsnapshot   # also copy it locally
```

MCP `pinchtab_memory_snapshot` takes `tabId`, `top` and `browser`; it takes the
snapshot and returns `{id, path, bytes, summary}` in one call.

## Summary

`GET /memory/snapshot/{snapshotId}/summary?top=20` returns `id`, `path`, `top`,
`nodeCount`, `edgeCount`, `totalSelfSize`, `constructors` (distinct constructor
count), `topBySize`, `topByCount` (constructor rows of `name`, `count`,
`selfSize`) and `duplicateStrings` (`value`, `length`, `count`, `selfSize`).

```bash
pinchtab memory summary heap_20260913_101010 --top 5
```

Objects group by constructor name; other V8 node types group as `(array)`,
`(string)`, `(closure)`, `(compiled code)`, `(system)` and so on, as in DevTools. A
JavaScript `Array`'s elements live in a separate `(array)` backing store, so a
growing array shows its bytes under `(array)` while `Array` itself stays small.

## Compare

`GET /memory/compare?base=<id>&head=<id>&top=20&retained=false`

| Parameter | Default | Description |
| --- | --- | --- |
| `base` | required | The earlier snapshot id |
| `head` | required | The later snapshot id |
| `top` | `20` | Rows in `constructors` and `newDuplicateStrings`; values above 200 are clamped to 200, a non-positive or non-integer value is `400 bad_top` |
| `retained` | `false` | Add `retainedSize` to each returned row, from a dominator tree of `head` |

```json
{
  "top": 20,
  "retained": false,
  "base": {"id": "heap_20260913_101010", "nodeCount": 51234, "edgeCount": 210876, "totalSelfSize": 2811904},
  "head": {"id": "heap_20260913_101042", "nodeCount": 51310, "edgeCount": 211002, "totalSelfSize": 18543616},
  "nodeDelta": 76,
  "sizeDelta": 15731712,
  "changed": 41,
  "constructors": [
    {"name": "(array)", "baseCount": 812, "headCount": 815, "countDelta": 3,
     "baseSelfSize": 190440, "headSelfSize": 15919128, "sizeDelta": 15728688}
  ],
  "newDuplicateStrings": [
    {"value": "session-expired", "length": 15, "count": 3, "selfSize": 96}
  ]
}
```

- `constructors` lists only constructors whose count or self size changed, largest
  absolute `sizeDelta` first, so both growth and release rise to the top. `changed`
  counts every changed constructor, including those `top` cut.
- `newDuplicateStrings` are strings held more than once in `head` that were not
  duplicated in `base`.
- `retainedSize` is the memory freed if every object of that constructor were
  collected: the sum of each object's dominator-tree subtree, counting an object
  nested under another of the same constructor once. It is present only with
  `retained=true`. Weak edges are ignored, as in DevTools. Computing it holds the
  whole head graph in memory and costs time proportional to its edges, which is why
  it is opt-in.
- Parsed snapshots are cached in memory per snapshot file for the process lifetime,
  so comparing the same ids again, or summarizing one you compared, does not reparse.

Snapshot ids match `^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`; the server names them
`heap_<YYYYMMDD_HHMMSS>`. Errors: `400 bad_snapshot_id` (missing or malformed id), `400 bad_top`,
`400 bad_retained`, `404 memory_snapshot_not_found` (details name the `id`),
`422 memory_snapshot_invalid` (details name the `id` and the broken `section`),
`403 memory_disabled`.

CLI and MCP:

```bash
pinchtab memory compare <base> <head> --top 5
pinchtab memory compare <base> <head> --retained --json
```

MCP `pinchtab_memory_compare` takes `base` and `head` (both required), `top`,
`retained` and `browser`.

Covered by `tests/e2e/scenarios/api/memory-basic.sh`,
`tests/e2e/scenarios/api/memory-extended.sh` and
`tests/e2e/scenarios/cli/memory-basic.sh`.

## Walkthrough: find a leak

1. Load the page and settle it, then take the baseline:
   `pinchtab memory snapshot` prints `heap_A`.
2. Repeat the suspect action several times (open and close a dialog, paginate,
   route back and forth). Repetition makes a leak grow linearly while one-off
   caches stay flat.
3. Take a second snapshot: `heap_B`. Taking a snapshot runs a full garbage
   collection, so anything still counted is reachable.
4. Compare: `pinchtab memory compare heap_A heap_B --top 10`. A leaking
   constructor sits near the top with a positive `sizeDelta` and a `countDelta`
   that tracks the number of repetitions.
5. Ask who holds it: `pinchtab memory compare heap_A heap_B --retained` shows
   how much each listed constructor keeps alive. A small object with a large
   retained size (a component, a closure, a `Map`) is usually the owner to fix.
6. Undo the action (close, release, navigate away), take `heap_C`, and compare
   `heap_B heap_C`: the constructor should show a negative `sizeDelta`. If it
   does not, something still references it.
7. Open the files in DevTools (Memory panel, Comparison view) for the retainer
   path of an individual object.
