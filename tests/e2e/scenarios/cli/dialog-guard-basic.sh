#!/bin/bash
# dialog-guard-basic.sh — `pinchtab nav` on a dialog-blocked tab fails at once
# with the dialog hint and remedy instead of a 60 s client timeout (PIN-425).

GROUP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${GROUP_DIR}/../../helpers/cli.sh"

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab nav on a dialog-blocked tab exits 1 immediately with the dialog hint"

pt_ok nav "${FIXTURES_URL}/buttons.html"

# Without --dialog-action the click leaves the alert pending on the tab.
pt click "#trigger-alert"

T0=$(get_time_ms)
pt nav "${FIXTURES_URL}/form.html"
T1=$(get_time_ms)
ELAPSED=$((T1 - T0))

assert_exit_code 1 "nav on a blocked tab exits 1"
if [ "$ELAPSED" -lt 5000 ]; then
  pass_assert "nav refused in ${ELAPSED}ms"
else
  fail_assert "nav took ${ELAPSED}ms, want an immediate refusal"
fi
NAV_ALL="${PT_OUT}${PT_ERR}"
if grep -q "dialog_blocked\|blocked by a JavaScript dialog" <<<"$NAV_ALL"; then
  pass_assert "nav names the blocking dialog"
else
  fail_assert "nav names the blocking dialog (got: $NAV_ALL)"
fi
if grep -q "pinchtab dialog accept" <<<"$NAV_ALL"; then
  pass_assert "nav prints the dialog remedy"
else
  fail_assert "nav prints the dialog remedy (got: $NAV_ALL)"
fi

end_test

# ─────────────────────────────────────────────────────────────────
start_test "pinchtab dialog accept unblocks nav"

pt_ok dialog accept
pt_ok nav "${FIXTURES_URL}/form.html"

end_test
