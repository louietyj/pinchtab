# PDF

Render the current page as a PDF.

```bash
curl "http://localhost:9867/pdf?output=file"
# Response: {"path":"/path/to/state/pdfs/page-20260308-120001.pdf","size":48210}

# CLI Alternative (human-readable by default)
pinchtab pdf -o page.pdf
# Output: Saved page.pdf (48210 bytes)

pinchtab pdf                        # Auto-generates filename: page-20260308-120001.pdf
```

## CLI Flags

| Flag | Description |
|------|-------------|
| `-o`, `--output` | Save PDF to file path |
| `--landscape` | Landscape orientation |
| `--scale` | Page scale (e.g. 0.5) |
| `--paper-width` | Paper width (inches) |
| `--paper-height` | Paper height (inches) |
| `--page-ranges` | Page ranges (e.g. 1-3) |
| `--prefer-css-page-size` | Use CSS page size |
| `--display-header-footer` | Show header/footer |
| `--header-template` | Header HTML template |
| `--footer-template` | Footer HTML template |
| `--margin-*` | Margins (top, bottom, left, right) |
| `--generate-tagged-pdf` | Generate tagged PDF |
| `--generate-document-outline` | Generate document outline |
| `--file-output` | Save server-side (`output=file`) instead of streaming bytes |
| `--path` | Server-side output path (under the state directory) |
| `--tab` | Target specific tab |

## API Parameters

`GET /pdf` and `POST /pdf` read query parameters; `POST /tabs/{id}/pdf` also
accepts them as JSON body fields.

| Parameter | Description |
|-----------|-------------|
| `output` | `file` to save server-side; response is `{path, size}` |
| `path` | Server-side path for `output=file`; must stay inside the state directory (else `400`) |
| `raw` | `true` for raw PDF bytes; otherwise the response is `{"format":"pdf","base64":"..."}` |
| `landscape` | Landscape orientation |
| `scale` | Page scale (default `1`) |
| `paperWidth`, `paperHeight` | Paper dimensions in inches (default `8.5` x `11`) |
| `marginTop`, `marginBottom`, `marginLeft`, `marginRight` | Margins in inches (default `0.4`) |
| `pageRanges` | Pages to export, e.g. `1-3,5` |
| `preferCSSPageSize`, `displayHeaderFooter`, `generateTaggedPDF`, `generateDocumentOutline` | Booleans |
| `headerTemplate`, `footerTemplate` | HTML templates; a template containing `<script`, `javascript:` or an `on*=` handler is rejected with `400` |
| `tabId` | Target a specific tab |

With IDPI content scanning enabled, a page that trips the scanner returns `403`.

MCP: `pinchtab_pdf` takes `tabId`, `landscape`, `scale`, `pageRanges` and returns the base64 PDF.

## Related Pages

- [Text](./text.md)
- [Screenshot](./screenshot.md)
