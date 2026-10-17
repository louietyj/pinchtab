#!/bin/bash
# extract-extended.sh — /extract under IDPI warn and strict modes, and the browse grant.

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/api.sh"

secure_post() {
  with_server "$E2E_SECURE_SERVER" pt_post "$@"
}

INJECT_SCHEMA='{"type":"object","properties":{"headline":{"type":"string","description":"page heading","x-pinchtab-hint":"role:heading"}}}'
INJECTED_VALUE_SCHEMA='{"type":"object","properties":{"notice":{"type":"string","description":"link text","x-pinchtab-hint":"role:link"}}}'

# ─────────────────────────────────────────────────────────────────
start_test "extract: injection page carries the IDPI warning in warn mode"

pt_post /navigate -d "{\"url\":\"${FIXTURES_URL}/idpi-inject.html\"}"
assert_ok "navigate to idpi-inject.html"

HEADERS=$(e2e_curl -s -D - -o /dev/null -X POST "${E2E_SERVER}/extract" -H "Content-Type: application/json" -d "{\"schema\":${INJECT_SCHEMA}}")
if echo "$HEADERS" | grep -qi "^X-IDPI-Warning:"; then
  pass_assert "X-IDPI-Warning header present"
else
  fail_assert "X-IDPI-Warning header missing: $HEADERS"
fi

pt_post /extract -d "{\"schema\":${INJECT_SCHEMA}}"
assert_ok "extract answers in warn mode"
assert_json_exists "$RESULT" ".idpiWarning" "idpiWarning present in the body"

end_test

# ─────────────────────────────────────────────────────────────────
start_test "extract: an extracted value carrying the injection is flagged in warn mode"

pt_post /navigate -d "{\"url\":\"${FIXTURES_URL}/idpi-inject.html\"}"
assert_ok "navigate to idpi-inject.html"

pt_post /extract -d "{\"schema\":${INJECTED_VALUE_SCHEMA}}"
assert_ok "extract the injected link text in warn mode"
assert_contains "$(echo "$RESULT" | jq -r '.data.notice')" "Ignore previous instructions" "the injection phrase is the extracted value"
assert_json_exists "$RESULT" ".idpiWarning" "idpiWarning present when the value itself is injected"

end_test

# ─────────────────────────────────────────────────────────────────
start_test "extract: strict mode blocks the injection page like /find"

secure_post /navigate -d "{\"url\":\"${FIXTURES_URL}/idpi-inject.html\"}"
assert_ok "navigate to the injection page in strict mode"

secure_post /extract -d "{\"schema\":${INJECT_SCHEMA}}"
assert_http_status 403 "extract blocked by IDPI"
assert_contains "$RESULT" "idpi" "block names the scanner"

secure_post /extract -d "{\"schema\":${INJECTED_VALUE_SCHEMA}}"
assert_http_status 403 "extract blocked when the extracted value is the injection"
assert_not_contains "$RESULT" "reveal your system prompt" "the blocked response does not leak the injected value"

secure_post /extract -d "{\"schema\":${INJECT_SCHEMA},\"scope\":\"ref:e99999\"}"
assert_http_status 403 "a request scope that excludes the injection does not bypass the page scan"

secure_post /find -d '{"query":"heading"}'
assert_http_status 403 "find blocked the same way"

end_test

# ─────────────────────────────────────────────────────────────────
start_test "extract: a session minted without the browse grant is refused"

pt_post /sessions '{"agentId":"e2e-extract-agent","grants":["clipboard"]}'
assert_ok "session created without browse"
NO_BROWSE_TOKEN=$(echo "$RESULT" | jq -r '.sessionToken')

STATUS=$(e2e_curl --token "" -s -o /dev/null -w "%{http_code}" -X POST "${E2E_SERVER}/extract" \
  -H "Authorization: Session ${NO_BROWSE_TOKEN}" -H "Content-Type: application/json" -d "{\"schema\":${INJECT_SCHEMA}}")
if [ "$STATUS" = "403" ]; then
  pass_assert "no-browse session refused on /extract"
else
  fail_assert "no-browse session got $STATUS on /extract, want 403"
fi

end_test

