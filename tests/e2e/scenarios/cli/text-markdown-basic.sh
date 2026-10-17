#!/bin/bash
# text-markdown-basic.sh — CLI surface of mode=markdown: pinchtab text --markdown
# prints Markdown, --output writes a file with a one-line confirmation, and
# --markdown --full is refused locally with a usage error.

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/cli.sh"

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab text --markdown prints Markdown structures"

pt_ok nav "${FIXTURES_URL}/markdown.html"
pt_ok text --markdown
assert_output_contains "# " "heading survives as Markdown"
assert_output_contains "## " "subheading survives as Markdown"
assert_output_contains "](https://example.com/link)" "inline link survives as Markdown"
assert_output_contains "- " "list item survives as Markdown"
assert_output_contains "|" "table survives as Markdown"

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab text --markdown --output writes the file and prints one line"

OUT="/tmp/page-$$.md"
rm -f "$OUT"
pt_ok text --markdown --output "$OUT"

if [ -s "$OUT" ]; then
  pass_assert "markdown file written"
else
  fail_assert "markdown file not written to $OUT"
fi

if grep -q "# " "$OUT"; then
  pass_assert "written file carries Markdown"
else
  fail_assert "written file has no Markdown heading"
fi

# stdout is a single confirmation line, not the page body.
LINE_COUNT=$(printf '%s' "$PT_OUT" | grep -c '.')
if [ "$LINE_COUNT" -le 1 ]; then
  pass_assert "stdout is a single confirmation line"
else
  fail_assert "stdout had $LINE_COUNT lines, expected one confirmation line"
fi
assert_output_contains "$OUT" "confirmation names the file"
assert_output_not_contains "](https://example.com/link)" "body did not flood stdout"

rm -f "$OUT"
end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab text --markdown --full is refused"

pt_fail text --markdown --full
if grep -qi "markdown" <<<"$PT_ERR"; then
  pass_assert "usage error names --markdown"
else
  fail_assert "usage error did not mention --markdown: $PT_ERR"
fi

end_test
