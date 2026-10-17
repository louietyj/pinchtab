# Frame

Get or set the current frame scope for selector-based snapshots and actions.

By default, selector lookup stays in the main document. To target iframe content with CSS, XPath, or text selectors, set the frame first.

Refs from `/snapshot` are different: if a snapshot includes same-origin iframe descendants, those refs can still be used directly without setting frame scope.

```bash
curl http://localhost:9867/frame

curl -X POST http://localhost:9867/frame \
  -H "Content-Type: application/json" \
  -d '{"target":"#payment-frame"}'

curl -X POST http://localhost:9867/frame \
  -H "Content-Type: application/json" \
  -d '{"target":"main"}'

# CLI Alternative
pinchtab frame                          # Shows: main (or frameId if scoped)
pinchtab frame "#payment-frame"         # Shows: <frameId> (<name>)
pinchtab frame main                     # Shows: main
pinchtab frame --json                   # Full JSON response
```

Targets accepted by `POST /frame` and `pinchtab frame`:

- `main` to clear frame scope
- a snapshot ref for an iframe owner
- a selector for an iframe element
- a frame name or frame URL

Response: `{tabId, scoped, target, current}` — unscoped, `target` and `current`
are `"main"`; scoped, `target` is the frame ID and `current`/`frame` carry
`{frameId, frameUrl, frameName, ownerRef}`. `POST` without `target` returns `400`;
a target that is not an iframe or frame returns `400`.

MCP: `pinchtab_frame` takes `target` (omit to read the current scope), `tabId`,
`browser`.

Typical iframe flow (API form exercised in `tests/e2e/scenarios/api/actions-extended.sh`):

```bash
pinchtab snap -i
pinchtab frame "#payment-frame"
pinchtab snap -i
pinchtab fill "#card-number" "4111111111111111"
pinchtab click "#pay-button"
pinchtab frame main
```

Notes:

- selector scope is explicit; unscoped selectors do not automatically pierce into iframes
- same-origin iframe content is supported; cross-origin iframe descendants are not currently exposed as frame scopes
- nested iframes usually require multiple `frame` hops
- the frame scope applies to `/snapshot`, `/capture`, selector-based `/action` calls, and `/text` when `frameId` is not provided explicitly
- a read served from a frame scope carries a `frame` object (`frameId`, `frameUrl`, `frameName`, `ownerRef`, `frameTitle`) so a later reader can tell the content is not the top document
- `/evaluate` is separate and does not inherit frame scope

## Related Pages

- [Snapshot](./snapshot.md)
- [Click](./click.md)
- [Fill](./fill.md)
