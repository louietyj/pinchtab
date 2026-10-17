# extract testdata

Accessibility snapshots used by `TestResolve_FromSnapshotFixtures` and the
`TestResolveArray_*` group tests. Each file is
the `nodes` array of a real `pinchtab snap --json`-shaped capture (the `GET
/snapshot` node list), so the unit tests run against the exact node shape the
e2e fixtures produce — including the `RootWebArea` root, the `StaticText`
children Chrome emits under each labelled paragraph, and the per-node `text`
filled by DOM-metadata enrichment.

| testdata file          | fixture                                   |
| ---------------------- | ----------------------------------------- |
| `extract-product.json` | `tests/e2e/fixtures/extract-product.html` |
| `extract-article.json` | `tests/e2e/fixtures/extract-article.html` |
| `extract-list.json`    | `tests/e2e/fixtures/extract-list.html`    |

## How these were produced

Captured from the e2e Docker stack (`tests/e2e/docker-compose.yml`, the
`pinchtab` + `fixtures` services) at PinchTab commit `dca3d7a9`
(`extract-list.json` at `f8f9dcb4`):

```
cd tests/e2e && docker compose up -d pinchtab fixtures
curl -s -X POST http://127.0.0.1:9999/navigate \
  -H 'Authorization: Bearer e2e-token' -H 'Content-Type: application/json' \
  -d '{"url":"http://fixtures:80/extract-product.html"}'
curl -s http://127.0.0.1:9999/snapshot -H 'Authorization: Bearer e2e-token'
# → the `nodes` array committed here, wrapped as {url, title, nodes}
```

All fixtures render identically headed and headless (no JS). Re-capture with the
same commands when a fixture changes shape; the volatile `nodeId`/`frameId`
fields differ per run and are not asserted (the tests key on `ref`, `role`,
`name`, `text`, `checked`). Note that the snapshot carries a single `rowgroup`:
in `extract-list.json` it holds the header row and every body
row, which is why the group detector drops header rows from the items rather
than relying on the rowgroup split.
