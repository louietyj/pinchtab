# Press

Send a keyboard key or chord to the current tab, optionally focusing an element first.

```bash
curl -X POST http://localhost:9867/action \
  -H "Content-Type: application/json" \
  -d '{"kind":"press","key":"Enter"}'
# CLI Alternative
pinchtab press Enter
# Response (use --json for full JSON)
OK
```

## CLI Flags

| Flag | Description |
|------|-------------|
| `--snap` | Output interactive snapshot after key press |
| `--snap-diff` | Output snapshot diff after key press |
| `--text` | Output page text after key press |
| `--json` | Full JSON response |
| `--tab` | Target specific tab |

Common keys include `Enter`, `Tab`, `Escape`, `ArrowDown`, `ArrowUp`, `Backspace`, `Delete`.

CLI usage is `pinchtab press [ref] <key|chord>`: with two arguments the first is a selector or ref that is focused before the key is sent (API: `ref` or `selector`).

A chord joins modifiers and a key with `+`, for example `Ctrl+A` or `Shift+ArrowLeft`. Modifiers are `Ctrl`/`Control`, `Alt`/`Option`, `Shift`, and `Meta`/`Cmd`/`Command`/`Super`/`Win` (case-insensitive); an unknown or repeated modifier is refused. The API alternatively takes the CDP bitmask `modifiers` (Alt=1, Ctrl=2, Meta=4, Shift=8) with a plain `key`, but not both forms at once.

## Related Pages

- [Click](./click.md)
- [Focus](./focus.md)
- [Keyboard](./keyboard.md)
