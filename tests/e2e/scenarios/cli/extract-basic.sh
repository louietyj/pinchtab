#!/bin/bash
# extract-basic.sh — CLI surface of POST /extract: pinchtab extract prints typed JSON
# from a schema file or stdin, --fields adds the ref table whose refs click without a
# snap, --scope and --max-items are forwarded, and a refused schema exits non-zero with
# the server's 400 message. The pinchtab and curl lines run as written in
# docs/reference/extract.md, from the directory holding the schema files.

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/cli.sh"

SCHEMAS="${GROUP_DIR}/../../fixtures/schemas"
PRODUCT_PAGE="${FIXTURES_URL}/extract-product.html"
LIST_PAGE="${FIXTURES_URL}/extract-list.html"

doc_run() {
  local line="$1" errfile
  errfile=$(mktemp)
  echo -e "  ${BLUE}→ (docs) ${line}${NC}"
  set +e
  PT_OUT=$(cd "$SCHEMAS" && PINCHTAB_SERVER="$E2E_SERVER" PINCHTAB_TOKEN="${E2E_SERVER_TOKEN:-}" bash -c "$line" 2>"$errfile")
  PT_CODE=$?
  set -e
  PT_ERR=$(cat "$errfile")
  rm -f "$errfile"
}

doc_ok() {
  doc_run "$1"
  if [ "$PT_CODE" -eq 0 ]; then
    pass_assert "exit 0: $1"
  else
    fail_assert "exit $PT_CODE: $1"
    echo -e "  ${RED}stderr: $PT_ERR${NC}"
  fi
}

assert_out_jq() {
  local expr="$1" want="$2" desc="$3" got
  got=$(jq -r "$expr" <<<"$PT_OUT" 2>/dev/null)
  if [ "$got" = "$want" ]; then
    pass_assert "$desc"
  else
    fail_assert "$desc (got '$got', want '$want')"
    echo -e "  ${RED}  output was: $PT_OUT${NC}"
  fi
}

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab extract --schema <file> prints typed JSON"

pt_ok nav "$PRODUCT_PAGE"
doc_ok 'pinchtab extract --schema product.schema.json'
assert_out_jq '.price' '1299' "price is 1299"
assert_out_jq '.price | type' 'number' "price is a JSON number"
assert_out_jq '.inStock' 'true' "inStock is true"
assert_out_jq '.inStock | type' 'boolean' "inStock is a JSON boolean"
assert_out_jq '.name' 'Sony WH-1000XM5 Wireless Headphones' "name is the fixture heading"
FILE_OUT="$PT_OUT"

doc_ok 'cat product.schema.json | pinchtab extract --schema -'
if [ "$PT_OUT" = "$FILE_OUT" ]; then
  pass_assert "--schema - from stdin prints the same data as the file"
else
  fail_assert "stdin output differs from the file output: $PT_OUT"
fi

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab extract --fields then click a field ref with no snap in between"

pt_ok nav "$PRODUCT_PAGE"
pt_ok snap
pt_ok nav "$PRODUCT_PAGE"
doc_ok 'pinchtab extract --schema product.schema.json --fields'
REF=$(awk -F'\t' '$1 == "name" {print $2}' <<<"$PT_OUT")
if [[ "$REF" =~ ^e[0-9]+$ ]]; then
  pass_assert "--fields lists name with ref $REF"
else
  fail_assert "--fields has no name<TAB>ref row: $PT_OUT"
fi
if awk -F'\t' '$1 == "price" && $2 ~ /^e[0-9]+$/ && $3 ~ /^(high|medium|low)$/ {found=1} END {exit !found}' <<<"$PT_OUT"; then
  pass_assert "--fields row carries ref and confidence for price"
else
  fail_assert "--fields has no price<TAB>ref<TAB>confidence row"
fi

pt click "$REF"
if [ "$PT_CODE" -eq 0 ] && ! grep -q "superseded" <<<"$PT_ERR$PT_OUT"; then
  pass_assert "click $REF after extract accepted with no snap in between"
else
  fail_assert "click $REF after extract refused (exit $PT_CODE): $PT_ERR"
fi

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab extract forwards --max-items and --scope"

pt_ok nav "$LIST_PAGE"
pt_ok extract --schema "$SCHEMAS/products.schema.json"
assert_out_jq '.products | length' '6' "uncapped grid has 6 products"

doc_ok 'pinchtab extract --schema products.schema.json --max-items 2'
assert_out_jq '.products | length' '2' "--max-items 2 caps the array"
if grep -q "truncated" <<<"$PT_ERR"; then
  pass_assert "truncation reported on stderr"
else
  fail_assert "no truncation note on stderr: $PT_ERR"
fi

pt_ok extract --schema "$SCHEMAS/amounts.schema.json"
assert_out_jq '.entries | length' '6' "unscoped amounts come from the 6-product grid"
doc_ok 'pinchtab extract --schema amounts.schema.json --scope role:table'
assert_out_jq '.entries | length' '5' "--scope role:table reads the 5 table rows"

pt_ok extract --schema "$SCHEMAS/amounts.schema.json" --scope role:table --json
TABLE_REF=$(jq -r '.fields.entries.ref' <<<"$PT_OUT")
pt_ok extract --schema "$SCHEMAS/amounts.schema.json" --scope "$TABLE_REF"
assert_out_jq '.entries | length' '5' "--scope with the bare ref ${TABLE_REF} reads the same 5 rows"

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab extract refuses a bad schema with the server's 400 message"

pt_ok nav "$PRODUCT_PAGE"
pt_fail extract --schema "$SCHEMAS/nested.schema.json"
if grep -q "properties.price.type" <<<"$PT_ERR"; then
  pass_assert "stderr names the offending schema path"
else
  fail_assert "stderr lacks the server's path: $PT_ERR"
fi

pt_fail extract --schema "$SCHEMAS/product.schema.json" --scope "css:main"
if grep -q "scope" <<<"$PT_ERR"; then
  pass_assert "a browser-selector scope is refused naming scope"
else
  fail_assert "scope refusal lacks the name: $PT_ERR"
fi

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab extract --json, --explain, --tab and a scope that matches nothing"

pt_ok nav "$PRODUCT_PAGE"
PRODUCT_TAB=$(echo "$PT_OUT" | tr -d '[:space:]')
pt_ok nav "$LIST_PAGE" --new-tab

pt_ok extract --schema "$SCHEMAS/product.schema.json" --tab "$PRODUCT_TAB" --json
assert_out_jq '.data.price' '1299' "--tab reads the product tab while the list tab is current"
assert_out_jq '.fields.name.ref | test("^e[0-9]+$")' 'true' "--json prints the envelope with field refs"
assert_out_jq '.vocabularyToken | length > 0' 'true' "--json envelope carries the vocabulary token"

pt_ok extract --schema "$SCHEMAS/product.schema.json" --tab "$PRODUCT_TAB" --explain
if awk -F'\t' '$1 == "price" && NF == 6 && $2 ~ /^e[0-9]+$/ && $4 ~ /^[0-9]+\.[0-9][0-9]$/ {found=1} END {exit !found}' <<<"$PT_OUT"; then
  pass_assert "--explain row has field, ref, confidence, score, source and reason columns"
else
  fail_assert "--explain has no six-column price row: $PT_OUT"
fi

pt_ok extract --schema "$SCHEMAS/product.schema.json" --tab "$PRODUCT_TAB" --scope ref:e99999
assert_out_jq '. == {}' 'true' "a scope that matches nothing extracts no data"
if grep -q "missing required fields: inStock, name, price" <<<"$PT_ERR"; then
  pass_assert "missing required fields reported on stderr with exit 0"
else
  fail_assert "no missing-required note on stderr: $PT_ERR"
fi

end_test

# ─────────────────────────────────────────────────────────────────
start_test "docs curl example returns typed data"

pt_ok nav "$PRODUCT_PAGE"
DOC_CURL='curl -X POST http://localhost:9867/extract \
  -H "Content-Type: application/json" \
  -d "{\"schema\":$(cat product.schema.json)}"'
doc_run "${DOC_CURL//http:\/\/localhost:9867/$E2E_SERVER} -s -H \"Authorization: Bearer ${E2E_SERVER_TOKEN:-}\""
assert_out_jq '.data.price' '1299' "curl example: price is 1299"
assert_out_jq '.fields.name.ref | test("^e[0-9]+$")' 'true' "curl example: name carries a ref"

end_test
