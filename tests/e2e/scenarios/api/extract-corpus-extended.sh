#!/bin/bash
# extract-corpus-extended.sh — the live /extract pipeline on each mirrored corpus page extracts what the offline unit corpus does.

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/api.sh"

CORPUS_DIR="${GROUP_DIR}/../../fixtures/corpus"
CORPUS_MANIFEST="${CORPUS_DIR}/manifest.json"

CORPUS_OUTCOMES='
def outcomes($got; $want; $prefix):
  $want | to_entries[] | .key as $k | .value as $w |
  if ($w | type) == "array" then
    (($got // {})[$k] // []) as $g |
    range(0; [($w | length), ($g | length)] | max) as $i |
    if $i >= ($w | length) then
      {path: "\($prefix)\($k)[\($i)]", hit: false, got: $g[$i], want: null}
    else
      outcomes($g[$i]; $w[$i]; "\($prefix)\($k)[\($i)].")
    end
  else
    {path: "\($prefix)\($k)", hit: ((($got // {})[$k]) == $w), got: (($got // {})[$k]), want: $w}
  end;
[outcomes($got; $want; "")]
'

CORPUS_DRIFT='
def pathname: map(if type == "number" then "[\(.)]" else ".\(.)" end) | join("") | ltrimstr(".");
[($live | paths(scalars)), ($offline | paths(scalars))] | unique |
map(. as $p | {path: ($p | pathname), live: ($live | getpath($p)), offline: ($offline | getpath($p))}) |
map(select(.live != .offline))
'

# ─────────────────────────────────────────────────────────────────
start_test "extract-corpus: live render, snapshot and resolve match the offline corpus"

for NAME in $(jq -r '.entries | keys[]' "$CORPUS_MANIFEST"); do
  OFFLINE_REASON=$(jq -r --arg n "$NAME" '.entries[$n].offline // empty' "$CORPUS_MANIFEST")
  if [ -n "$OFFLINE_REASON" ]; then
    skip_assert "${NAME}: offline-only, skipped: ${OFFLINE_REASON}"
    continue
  fi
  OFFLINE_HITS=$(jq -r --arg n "$NAME" '.entries[$n].hits' "$CORPUS_MANIFEST")
  OFFLINE_DATA=$(jq -c --arg n "$NAME" '.entries[$n].data' "$CORPUS_MANIFEST")

  pt_post /navigate -d "{\"url\":\"${FIXTURES_URL}/corpus/${NAME}.html\"}" >/dev/null
  assert_ok "${NAME}: navigate to corpus/${NAME}.html"

  SCHEMA=$(jq -c . "${CORPUS_DIR}/${NAME}.schema.json")
  pt_post /extract -d "{\"schema\":${SCHEMA}}" >/dev/null
  assert_ok "${NAME}: extract"

  OUTCOMES=$(echo "$RESULT" | jq -c --slurpfile want "${CORPUS_DIR}/${NAME}.expected.json" \
    '.data as $got | $want[0] as $want | '"$CORPUS_OUTCOMES")
  echo "$OUTCOMES" | jq -r '.[] | "    \(if .hit then "hit " else "miss" end) \(.path) got=\(.got | tojson) want=\(.want | tojson)" | .[0:160]'
  HITS=$(echo "$OUTCOMES" | jq '[.[] | select(.hit)] | length')
  TOTAL=$(echo "$OUTCOMES" | jq 'length')

  SAME=$(echo "$RESULT" | jq --argjson offline "$OFFLINE_DATA" '.data == $offline')
  DRIFT=$(echo "$RESULT" | jq -c --argjson offline "$OFFLINE_DATA" '.data as $live | '"$CORPUS_DRIFT")
  echo "$DRIFT" | jq -r '.[] | "    drift \(.path) live=\(.live | tojson) offline=\(.offline | tojson)" | .[0:160]'
  DRIFTED=$(echo "$DRIFT" | jq 'length')

  if [ "$HITS" -eq "$OFFLINE_HITS" ] && [ "$SAME" = "true" ]; then
    pass_assert "${NAME}: pass, ${HITS}/${TOTAL} fields match expected.json and every field equals the offline run"
  else
    fail_assert "${NAME}: fail, ${HITS}/${TOTAL} fields match expected.json (offline ${OFFLINE_HITS}), ${DRIFTED} fields differ from the offline run"
  fi
done

end_test
