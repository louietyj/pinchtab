# e2e extract corpus

Static copies of the pages in `internal/extract/testdata/corpus/`, used by
`tests/e2e/scenarios/api/extract-corpus-extended.sh`. That README describes how
they were made and how `manifest.json` gates them.

## Attribution and licences

All pages captured on 2026-09-13.

| file           | source                                                                                               | author / licence                                                                 |
| -------------- | ---------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `article.html` | https://en.wikinews.org/wiki/Grateful_Dead_rhythm_guitarist_Bob_Weir_dies,_aged_78                   | Wikinews contributors, CC BY 4.0 (https://creativecommons.org/licenses/by/4.0/)    |
| `infobox.html` | https://en.wikipedia.org/wiki/Mount_Twynam                                                           | Wikipedia contributors, CC BY-SA 4.0 (https://creativecommons.org/licenses/by-sa/4.0/) |
| `search.html`  | https://en.wikipedia.org/w/index.php?search=headless+browser&title=Special:Search&profile=advanced&fulltext=1&ns0=1 | Wikipedia contributors (article excerpts), CC BY-SA 4.0                          |
| `product.html` | https://books.toscrape.com/catalogue/a-light-in-the-attic_1000/index.html                            | Zyte's public scraping sandbox                                                   |
| `listing.html` | https://books.toscrape.com/catalogue/category/books/travel_2/index.html                              | Zyte's public scraping sandbox                                                   |
| `login.html`   | https://quotes.toscrape.com/login                                                                    | Zyte's public scraping sandbox                                                   |

Changes from the originals: scripts, iframes and external resources removed,
stylesheets reduced to the matching rules and inlined, images replaced by a
1x1 pixel. No text was edited. The Wikipedia-derived copies are adaptations
shared under CC BY-SA 4.0.
