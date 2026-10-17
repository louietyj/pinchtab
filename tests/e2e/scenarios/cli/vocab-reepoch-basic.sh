#!/bin/bash
# vocab-reepoch-basic.sh — CLI: every verb that can re-epoch a tab's ref cache
# (capture, annotate, screenshot --annotate, find in all three render paths, and
# an action with a semantic selector) captures the vocabulary token it minted,
# so the next `click <ref>` echoes it instead of a stale one.
#
# Each test files a token with `snap`, navigates again (which drops the tab's
# ref cache), runs the verb, then clicks with no snap in between. Without the
# capture the click echoes the snap's token and is refused 409 vocab_superseded.

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/cli.sh"

PAGE="${FIXTURES_URL}/buttons.html"

stale_token_on_fresh_page() {
  pt_ok nav "$PAGE"
  pt_ok snap
  pt_ok nav "$PAGE"
}

assert_ref() {
  local ref="$1" verb="$2"
  if [ -n "$ref" ] && [ "$ref" != "null" ]; then
    pass_assert "$verb returned ref $ref for Increment"
  else
    fail_assert "$verb returned no ref for Increment"
  fi
}

click_ref_increments() {
  local ref="$1" verb="$2"
  pt click "$ref"
  if [ "$PT_CODE" -eq 0 ] && ! grep -q "superseded" <<<"$PT_ERR$PT_OUT"; then
    pass_assert "click $ref after $verb accepted with no snap in between"
  else
    fail_assert "click $ref after $verb refused (exit $PT_CODE): $PT_ERR"
  fi
  pt_ok eval "document.getElementById('count').textContent"
  assert_output_contains "1" "click after $verb reached the Increment button"
}

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab find --ref-only then click on a fresh page"

stale_token_on_fresh_page
pt_ok find "Increment" --ref-only
REF=$(head -n1 <<<"$PT_OUT" | tr -d '[:space:]')
assert_ref "$REF" "find --ref-only"
click_ref_increments "$REF" "find --ref-only"

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab find --json then click on a fresh page"

stale_token_on_fresh_page
pt_ok find "Increment" --json
REF=$(jq -r '.best_ref // empty' <<<"$PT_OUT")
assert_ref "$REF" "find --json"
click_ref_increments "$REF" "find --json"

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab find (terse) then click on a fresh page"

stale_token_on_fresh_page
pt_ok find "Increment"
REF=$(grep -m1 'Increment' <<<"$PT_OUT" | awk '{print $1}')
assert_ref "$REF" "find (terse)"
click_ref_increments "$REF" "find (terse)"

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab capture then click on a fresh page"

stale_token_on_fresh_page
OUT_FILE="/tmp/e2e-vocab-capture.jpg"
pt_ok capture --json --wait none -o "$OUT_FILE"
rm -f "$OUT_FILE"
REF=$(jq -r 'first(.snapshot.nodes[] | select(.name == "Increment") | .ref) // empty' <<<"$PT_OUT")
assert_ref "$REF" "capture"
click_ref_increments "$REF" "capture"

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab annotate then click on a fresh page"

stale_token_on_fresh_page
pt_ok annotate
REF=$(grep -m1 '"Increment"' <<<"$PT_OUT" | awk '{print $1}')
assert_ref "$REF" "annotate"
click_ref_increments "$REF" "annotate"
pt annotate --clear

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab screenshot --annotate then click on a fresh page"

stale_token_on_fresh_page
OUT_FILE="/tmp/e2e-vocab-annotated.png"
pt_ok screenshot --annotate -o "$OUT_FILE"
rm -f "$OUT_FILE"
REF=$(grep -m1 '"Increment"' <<<"$PT_OUT" | awk '{print $2}')
assert_ref "$REF" "screenshot --annotate"
click_ref_increments "$REF" "screenshot --annotate"

end_test

# ─────────────────────────────────────────────────────────────────
# The semantic hover re-epochs the dropped cache with the interactive filter, so
# the ref comes from an interactive snap of the same DOM taken before the
# second nav; the token the click must echo is the one the hover minted.
start_test "pinchtab hover semantic:<name> then click on a fresh page"

pt_ok nav "$PAGE"
pt_ok snap --interactive --compact=false
REF=$(find_ref_by_name "Increment" "$PT_OUT")
assert_ref "$REF" "snap --interactive"
pt_ok nav "$PAGE"
pt_ok hover "semantic:Increment"
click_ref_increments "$REF" "hover semantic:Increment"

end_test
