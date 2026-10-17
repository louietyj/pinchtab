# Verification and Gotchas

Read this reference when a normal `snap` or `text` result is insufficient, when an action appears to succeed but the outcome is unclear, or when working with frames and dynamic pages.

## Verify outcomes

- `text` confirms success messages and navigation outcomes. It is Readability-filtered, so it can omit navigation, repeated headlines, short nodes, and collapsed lists. Use `text --full` when the expected marker is short or missing.
- `{"clicked":true,"submitted":true}` means the browser event fired; it does not prove that the server accepted the action or that validation passed. Verify with `--snap-diff`, a fresh `snap`, or `text`.
- Refs are stale after navigation or a significant DOM update. Fetch fresh refs rather than retrying an old one.
- A stale ref is not always refused: after a navigation the server may "recover" it onto the closest match on the new page and still print `OK`. When a click reported `navigated`/`refsStale`, snap again before the next ref action; `--json` shows a `recovery` block when this happened. `409 vocab_superseded` means a newer snapshot replaced your refs — re-snapshot.
- While a JavaScript dialog is open, every page command (`snap`, `text`, `click`, `nav`, …) fails immediately with `409 dialog_blocked` until you run `pinchtab dialog accept [text]` or `pinchtab dialog dismiss`.

## Frames, visibility, and selectors

- Default `snap` flattens same-origin iframe descendants, so ref-based actions work across those frame boundaries. Use `frame` only for scoped reads; it accepts `main`, an iframe ref, CSS, a frame name, or a URL.
- Cross-origin iframes are not exposed as frame scopes. Do not attempt to bypass that boundary.
- `text` can include `display:none` and `visibility:hidden` content. Use `snap` to confirm visible controls.
- `snap -i -c` omits non-interactive descendants. Use a frame scope or full `snap` when those nodes matter.
- Compact snapshots show `<option>` labels, not necessarily values. `select` accepts a value or visible text.
- `text:<value>` selectors can be unreliable on large pages. Prefer a fresh accessibility ref from `snap -i -c`.
- `aria-expanded` is usually on an accordion or menu container rather than its click target; inspect the wrapper when verifying state.

## Authorized JavaScript diagnostics

Use `eval` only under the main skill's authorization rules. Wrap expressions that declare identifiers in an IIFE because top-level `const`, `let`, and `class` declarations persist in the shared realm:

```bash
pinchtab eval "(() => { const r = document.querySelector('#x').getBoundingClientRect(); return {x: r.x, y: r.y, w: r.width, h: r.height}; })()"
```

Single expressions without declarations, such as `document.title`, do not need an IIFE.
