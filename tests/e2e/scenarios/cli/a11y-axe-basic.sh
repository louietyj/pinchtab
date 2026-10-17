#!/bin/bash
# a11y-axe-basic.sh — CLI: pinchtab a11y audit --axe yields the same violation
# ids as the API, and --tags filters the run.

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/cli.sh"

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab a11y audit --axe --json"

pt_ok nav "${FIXTURES_URL}/a11y-violations.html"

pt_ok a11y audit --axe --json
assert_output_contains '"engine": "axe"' "engine echoes axe"
assert_output_contains '"version": "4.13.0"' "axe version echoed"
for want in image-alt label color-contrast; do
  assert_output_contains "\"$want\"" "violation $want present"
done

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab a11y audit --axe --tags wcag2a filters"

pt_ok a11y audit --axe --tags wcag2a --json
assert_output_contains '"image-alt"' "wcag2a keeps image-alt"
if grep -q '"color-contrast"' <<<"$PT_OUT"; then
  fail_assert "color-contrast (wcag2aa) should be dropped under --tags wcag2a"
else
  pass_assert "color-contrast absent under --tags wcag2a"
fi

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab a11y audit (native) summary"

pt_ok a11y audit
assert_output_contains "engine=native" "native engine summary line"

end_test

# ─────────────────────────────────────────────────────────────────
# snapshot files a token, the second nav invalidates it, and the audit must
# overwrite the store with the token it minted; otherwise the click echoes the
# snapshot's stale token and the server refuses it 409 vocab_superseded.
start_test "pinchtab a11y audit --axe refreshes the vocab store so click on a returned ref succeeds"

pt_ok nav "${FIXTURES_URL}/a11y-violations.html"
pt_ok snap
pt_ok nav "${FIXTURES_URL}/a11y-violations.html"

pt_ok a11y audit --axe --json
REF=$(jq -r 'first(.violations[] | select(.id=="label") | .nodes[] | select(.ref!=null and .ref!="") | .ref)' <<<"$PT_OUT")
if [ -n "$REF" ] && [ "$REF" != "null" ]; then
  pass_assert "label violation carries ref $REF"
else
  fail_assert "label violation carries no ref"
fi

pt_ok click "$REF"
if grep -q "superseded" <<<"$PT_ERR$PT_OUT"; then
  fail_assert "click echoed a stale token: $PT_ERR"
else
  pass_assert "click on $REF accepted with the audit's token"
fi

end_test
