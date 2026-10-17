#!/bin/bash
# memory-basic.sh — CLI: pinchtab memory reads heap usage, memory snapshot writes a
# heap snapshot, and memory summary prints its constructor table. The default e2e
# server runs with security.allowMemory on.

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/cli.sh"

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab memory --json"

pt_ok nav "${FIXTURES_URL}/memory-leak.html"

pt_ok memory --json
for key in usedJSHeapSize totalJSHeapSize jsHeapSizeLimit documents nodes listeners frames; do
  assert_output_contains "\"$key\"" "usage reports $key"
done

pt_ok memory --gc
assert_output_contains "heap used" "human summary line"
assert_output_contains "(after gc)" "summary says it collected first"

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab memory snapshot + summary --top 5"

pt_ok click "#leak"

pt_ok memory snapshot
assert_output_contains "Heap snapshot heap_" "snapshot prints its id"
assert_output_contains ".heapsnapshot" "snapshot prints the file path"
SNAP_ID=$(grep -o 'pinchtab memory summary [^ ]*' <<<"$PT_OUT" | awk '{print $4}')
if [ -n "$SNAP_ID" ]; then
  pass_assert "snapshot id parsed: $SNAP_ID"
else
  fail_assert "no snapshot id in the output"
fi

pt_ok memory summary "$SNAP_ID" --top 5
assert_output_contains "CONSTRUCTOR" "summary prints the constructor table header"
assert_output_contains "Top constructors by self size" "size table present"
assert_output_contains "Top constructors by count" "count table present"

pt_ok memory summary "$SNAP_ID" --top 5 --json
assert_output_contains '"topBySize"' "json summary carries topBySize"

pt_ok click "#release"

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab memory compare <a> <b> --top 5"

snapshot_id() {
  grep -o 'pinchtab memory summary [^ ]*' <<<"$PT_OUT" | awk '{print $4}'
}

pt_ok memory snapshot
BASE_ID=$(snapshot_id)

pt_ok click "#leak"
pt_ok click "#leak"
pt_ok click "#leak"

pt_ok memory snapshot
HEAD_ID=$(snapshot_id)
if [ -n "$BASE_ID" ] && [ -n "$HEAD_ID" ] && [ "$BASE_ID" != "$HEAD_ID" ]; then
  pass_assert "two snapshot ids: $BASE_ID → $HEAD_ID"
else
  fail_assert "snapshot ids '$BASE_ID' and '$HEAD_ID' are missing or equal"
fi

pt_ok memory compare "$BASE_ID" "$HEAD_ID" --top 5 --json
JSON_TOP=$(echo "$PT_OUT" | jq -r '.constructors[0].name')
if [ "$JSON_TOP" = "(array)" ] && echo "$PT_OUT" | jq -e '.constructors[0].sizeDelta >= 10485760' >/dev/null; then
  pass_assert "json top row is (array) grown by at least 10 MB"
else
  fail_assert "json top row is '$JSON_TOP', want (array) grown by at least 10 MB"
fi

pt_ok memory compare "$BASE_ID" "$HEAD_ID" --top 5
assert_output_contains "$BASE_ID → $HEAD_ID" "header names both snapshots"
assert_output_contains "CONSTRUCTOR" "delta table header"
FIRST_ROW=$(grep -A1 'CONSTRUCTOR' <<<"$PT_OUT" | tail -1 | awk '{print $1}')
if [ "$FIRST_ROW" = "$JSON_TOP" ]; then
  pass_assert "human table prints the same top row ($FIRST_ROW)"
else
  fail_assert "human top row '$FIRST_ROW' differs from json top row '$JSON_TOP'"
fi

pt_ok memory compare "$BASE_ID" "$HEAD_ID" --top 5 --retained
assert_output_contains "RETAINED" "--retained adds the retained column"

pt_ok memory compare "$BASE_ID" "$HEAD_ID" --top 5 --json
if echo "$PT_OUT" | jq -e '[.constructors[] | has("retainedSize")] | any | not' >/dev/null; then
  pass_assert "no retainedSize without --retained"
else
  fail_assert "retainedSize present without --retained"
fi

pt_fail memory compare "$BASE_ID" heap_does_not_exist
if grep -q "404" <<<"$PT_ERR" && grep -q "heap_does_not_exist" <<<"$PT_ERR"; then
  pass_assert "unknown head id reports 404 naming the id"
else
  fail_assert "unknown head id stderr lacks 404 or the id: $PT_ERR"
fi

pt_ok click "#release"

end_test
