package main

import "testing"

func TestStateSaveTakesTheNameAsArgumentOrFlag(t *testing.T) {
	checkOperandRows(t, stateSaveCmd, "name", []operandRow{
		{name: "positional name", argv: []string{"walk1"}, want: "walk1"},
		{name: "--name alias", argv: []string{"--name", "walk1", "--encrypt"}, want: "walk1"},
		{name: "no name auto-generates", argv: nil, want: ""},
		bothGivenRow("name"),
		{name: "two names", argv: []string{"a", "b"}, refuses: []string{"at most 1"}},
	})
}

func TestStateLoadTakesTheNameOrPrefixAsArgumentOrFlag(t *testing.T) {
	checkOperandRows(t, stateLoadCmd, "name", []operandRow{
		{name: "positional name", argv: []string{"walk1"}, want: "walk1"},
		{name: "positional prefix", argv: []string{"wal"}, want: "wal"},
		{name: "--name alias", argv: []string{"--name", "walk1"}, want: "walk1"},
		{name: "no name is refused", argv: nil, refuses: []string{"needs a name", "pinchtab state list"}},
		bothGivenRow("name"),
		{name: "two names", argv: []string{"a", "b"}, refuses: []string{"at most 1"}},
	})
}

func TestStateShowNeedsExactlyOneName(t *testing.T) {
	checkOperandRows(t, stateShowCmd, "name", []operandRow{
		{name: "positional name", argv: []string{"walk1"}, want: "walk1"},
		{name: "--name alias", argv: []string{"--name", "walk1"}, want: "walk1"},
		{name: "no name is refused", argv: nil, refuses: []string{"needs a name", "pinchtab state show <name>"}},
		bothGivenRow("name"),
		{name: "two names", argv: []string{"a", "b"}, refuses: []string{"at most 1"}},
	})
}

func TestStateDeleteNeedsExactlyOneName(t *testing.T) {
	checkOperandRows(t, stateDeleteCmd, "name", []operandRow{
		{name: "positional name", argv: []string{"walk1"}, want: "walk1"},
		{name: "--name alias", argv: []string{"--name", "walk1"}, want: "walk1"},
		{name: "no name is refused", argv: nil, refuses: []string{"needs a name", "pinchtab state delete <name>"}},
		bothGivenRow("name"),
		{name: "two names", argv: []string{"a", "b"}, refuses: []string{"at most 1"}},
	})
}
