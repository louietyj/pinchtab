# Fill

Set an input value directly without relying on the same event sequence as `type`.

```bash
curl -X POST http://localhost:9867/action \
  -H "Content-Type: application/json" \
  -d '{"kind":"fill","ref":"e8","text":"ada@pinchtab.com"}'
# CLI Alternative
pinchtab fill e8 "ada@pinchtab.com"
# Response (use --json for full JSON)
OK
```

## CLI Flags

| Flag | Description |
|------|-------------|
| `--submit` | Press Enter after filling the field (`submit:true` in the action JSON) |
| `--snap` | Output interactive snapshot after fill |
| `--snap-diff` | Output snapshot diff after fill |
| `--text` | Output page text after fill |
| `--json` | Full JSON response |
| `--tab` | Target specific tab |

## Examples

```bash
pinchtab fill e8 "ada@pinchtab.com"     # Fill by ref
pinchtab fill "#email" "user@example.com"  # Fill by CSS
pinchtab fill "text:Email" "test@test.com" # Fill by text selector
pinchtab fill e8 "value" --snap         # Fill and show snapshot
```

## Notes

- Accepts unified selectors: `e8`, `#email`, `xpath://...`, `text:Email`
- Refs for iframe descendants can be filled directly without frame switch
- Selector lookup is limited to current frame scope (default: `main`)
- Use [`/frame`](./frame.md) before selector-based iframe fills
- Missing selectors fail immediately; use `pinchtab wait` first for async fields (see [`commands.md`](../commands.md))
- For API, use `selector` field for CSS/XPath/text selectors
- The API reads `text`, falling back to `value`. A fill with neither is refused with `400 missing_fill_text`; send `"text":""` to clear the field

## Related Pages

- [Frame](./frame.md)
- [Type](./type.md)
- [Snapshot](./snapshot.md)
