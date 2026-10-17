# extract corpus

Real accessibility snapshots of public pages, each with a JSON schema and the
values a person reading the page would expect back. `TestCorpus` runs
`Resolve` over every entry, logs a hit or miss per field and prints the overall
precision; the test fails when precision drops below `baseline.txt`.

```
go test ./internal/extract/ -run TestCorpus -v
```

Each entry is a directory `<name>/` holding:

| file            | contents                                                                                      |
| --------------- | --------------------------------------------------------------------------------------------- |
| `snapshot.json` | the `pinchtab snap --full` output (`{url, title, nodes}`), one node per line, otherwise verbatim |
| `schema.json`   | the schema sent to `POST /extract`                                                            |
| `expected.json` | the ground truth read off the page, not the extractor's current output                        |

## Scoring

Every leaf of `expected.json` is one scored field. A field is a hit when the
extracted value equals the expected value after a JSON round trip (so `51.77`
and `0` compare as numbers). Arrays are scored item by item: each field of each
expected item counts, and every extra item the extractor returns counts as one
miss. Precision is hits divided by scored fields, rounded to four decimals, over
the whole corpus.

## The gate

`baseline.txt` holds the precision measured when the corpus was committed. A
change that lowers precision fails `TestCorpus`. A change that raises it should
raise `baseline.txt` to the new figure in the same commit, so the gain cannot be
lost silently later.

## Entries

All captured on 2026-09-13 at PinchTab `52bcc08d`, headless Chrome 144, with the
tab viewport fixed at 1280x800, device scale factor 1.

| entry      | shape                          | URL                                                                                                  | e2e mirror    |
| ---------- | ------------------------------ | ---------------------------------------------------------------------------------------------------- | ------------- |
| `product`  | e-commerce product page        | https://books.toscrape.com/catalogue/a-light-in-the-attic_1000/index.html                            | yes           |
| `listing`  | product listing grid           | https://books.toscrape.com/catalogue/category/books/travel_2/index.html                              | yes           |
| `article`  | news article                   | https://en.wikinews.org/wiki/Grateful_Dead_rhythm_guitarist_Bob_Weir_dies,_aged_78                   | yes           |
| `infobox`  | Wikipedia infobox table        | https://en.wikipedia.org/wiki/Mount_Twynam                                                           | yes           |
| `login`    | login form                     | https://quotes.toscrape.com/login                                                                    | yes           |
| `search`   | search results                 | https://en.wikipedia.org/w/index.php?search=headless+browser&title=Special:Search&profile=advanced&fulltext=1&ns0=1 | yes |
| `pricing`  | pricing page                   | https://github.com/pricing                                                                           | offline-only  |
| `stories`  | ranked story list              | https://news.ycombinator.com/news                                                                    | offline-only  |

`listing`, `search`, `stories` and `pricing` exercise arrays of objects;
`product`, `article`, `infobox` and `login` exercise `x-pinchtab-hint`.

The card's GitHub issues list was replaced by the Hacker News front page: on
`github.com/pinchtab/pinchtab/issues` the issue rows render in the DOM but are
absent from the `snap --full` node list, so no issue could be scored.

## Recapturing

Run a bridge, open the page in a new tab, fix the viewport, reload, and snapshot
(add `-H "Authorization: Bearer <token>"` to each curl when the bridge has a
token):

```
pinchtab bridge --port 19871 &
TAB=$(curl -s -X POST localhost:19871/navigate -d '{"url":"<URL>","newTab":true}' | jq -r .tabId)
curl -s -X POST localhost:19871/tabs/$TAB/emulation/viewport -d '{"width":1280,"height":800,"deviceScaleFactor":1}'
curl -s -X POST localhost:19871/navigate -d "{\"url\":\"<URL>\",\"tabId\":\"$TAB\"}"
pinchtab --server http://127.0.0.1:19871 snap --full --tab $TAB > raw.json
```

`--full` matters: `snap --json` keeps the interactive filter, while `/extract`
resolves against the unfiltered node list. Rewrite `raw.json` into
`snapshot.json` as `{url, title, nodes}` with one node per line
(`jq -c '.nodes[]'`). A recapture moves refs and page content, so re-read
`expected.json` from the page and re-measure `baseline.txt` in the same commit.

## e2e mirror

`tests/e2e/fixtures/corpus/` holds, for each mirrored entry, a static copy of
the rendered page (`<name>.html`: scripts, iframes and external resources
removed, the stylesheets reduced to the rules that match the page and inlined,
images replaced by a 1x1 pixel) plus byte-identical copies of `schema.json` and
`expected.json`. Each copy was checked to produce the same node list (depth,
role, name, text, value) as the committed snapshot. Its `manifest.json` gives
each mirrored entry the offline run's `hits` against `expected.json` and the
`data` it extracts, and each offline-only entry the reason it is not mirrored.
`TestCorpus_E2EMirrorMatchesUnitCorpus` keeps the copies and the manifest in
step with this directory and fails when a recorded `hits` or `data` differs
from the offline run. After a change that moves the offline result, regenerate
the manifest rather than editing it:

```
go test ./internal/extract/ -run TestCorpus_E2EMirror -update
```

The `tests/e2e/scenarios/api/extract-corpus-extended.sh` scenario runs each
mirrored page through the live pipeline, reports every field, and fails an
entry whose live hits differ from the recorded `hits` or whose live `data`
differs from the recorded `data`, printing each field that drifted.

## Attribution and licences

The snapshots here and the HTML copies under `tests/e2e/fixtures/corpus/` carry
third-party page content, captured on 2026-09-13 from the URLs in the table
above:

- `article`: "Grateful Dead rhythm guitarist Bob Weir dies, aged 78", Wikinews
  contributors, licensed CC BY 4.0
  (https://creativecommons.org/licenses/by/4.0/; the page's own footer puts
  text created after 2024-12-16 under 4.0). Attributed to Wikinews.
- `infobox`: "Mount Twynam", Wikipedia contributors, and `search`: the
  Wikipedia search results page for "headless browser", whose snippets are
  excerpts of Wikipedia articles by Wikipedia contributors; both licensed
  CC BY-SA 4.0 (https://creativecommons.org/licenses/by-sa/4.0/). The copies
  are adaptations and are shared under the same licence.
- `product`, `listing`, `login`: Zyte's public scraping sandboxes
  (books.toscrape.com, quotes.toscrape.com), published for scraping practice.
- `pricing`, `stories`: snapshot only, no HTML copy; see the e2e manifest.

Changes from the originals: the snapshots are reformatted to one node per line;
the HTML copies have scripts, iframes and external resources removed,
stylesheets reduced and inlined, and images replaced by a 1x1 pixel. No text
was edited.
