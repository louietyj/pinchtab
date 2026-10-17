package extract

import (
	"strconv"
	"strings"

	"github.com/pinchtab/pinchtab/internal/bridge/observe"
)

func coerceNumber(s string) (float64, bool) {
	var b strings.Builder
	sign := ""
	seenDigit := false
	seenDot := false
	done := false
	for _, r := range s {
		if done {
			break
		}
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			seenDigit = true
		case r == '.' && !seenDot:
			b.WriteRune('.')
			seenDot = true
		case r == ',' && seenDigit:
		case isMinus(r) && !seenDigit && sign == "":
			sign = "-"
		case r == '+' && !seenDigit && sign == "":
			sign = "+"
		default:
			if seenDigit {
				done = true
			}
		}
	}
	if !seenDigit {
		return 0, false
	}
	f, err := strconv.ParseFloat(sign+b.String(), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
func isMinus(r rune) bool {
	return r == '-' || r == '−'
}
func coerceBool(node observe.A11yNode, value string) (bool, bool) {
	switch node.Checked {
	case observe.CheckedTrue:
		return true, true
	case observe.CheckedFalse:
		return false, true
	case observe.CheckedMixed:
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes":
		return true, true
	case "false", "no":
		return false, true
	}
	return false, false
}
