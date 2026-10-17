package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type operandRow struct {
	name    string
	argv    []string
	want    string
	refuses []string
}

func resolveOperand(t *testing.T, verb *cobra.Command, flag string, argv ...string) (string, error) {
	t.Helper()
	t.Cleanup(func() {
		verb.Flags().VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
	})
	if err := verb.ParseFlags(argv); err != nil {
		t.Fatalf("%s cannot parse %v: %v", verb.CommandPath(), argv, err)
	}
	args := verb.Flags().Args()
	if err := verb.ValidateArgs(args); err != nil {
		return "", err
	}
	return operandOrFlag(verb, args, flag), nil
}

func checkOperandRows(t *testing.T, verb *cobra.Command, flag string, rows []operandRow) {
	t.Helper()
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			got, err := resolveOperand(t, verb, flag, row.argv...)
			invocation := verb.CommandPath() + " " + strings.Join(row.argv, " ")
			if len(row.refuses) > 0 {
				if err == nil {
					t.Fatalf("`%s` was accepted with %s %q, want a refusal", invocation, flag, got)
				}
				for _, want := range row.refuses {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("`%s` refusal %q does not say %q", invocation, err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("`%s` was refused: %v", invocation, err)
			}
			if got != row.want {
				t.Errorf("`%s` resolved %s %q, want %q", invocation, flag, got, row.want)
			}
		})
	}
}

func bothGivenRow(flag string) operandRow {
	return operandRow{
		name:    "argument and --" + flag + " together",
		argv:    []string{"k1", "--" + flag, "k2"},
		refuses: []string{"twice", `"k1"`, `"k2"`, "--" + flag},
	}
}

func TestStorageGetTakesTheKeyAsArgumentOrFlag(t *testing.T) {
	checkOperandRows(t, storageGetCmd, "key", []operandRow{
		{name: "positional key", argv: []string{"k1"}, want: "k1"},
		{name: "--key alias", argv: []string{"--key", "k1"}, want: "k1"},
		{name: "no key lists the store", argv: nil, want: ""},
		bothGivenRow("key"),
		{name: "two keys", argv: []string{"k1", "k2"}, refuses: []string{"at most 1"}},
	})
}

func TestStorageDeleteNeedsExactlyOneKeyAndNeverWipes(t *testing.T) {
	checkOperandRows(t, storageDeleteCmd, "key", []operandRow{
		{name: "positional key", argv: []string{"k1"}, want: "k1"},
		{name: "--key alias", argv: []string{"--key", "k1", "--type", "session"}, want: "k1"},
		{name: "bare delete is refused, naming the wipe verb", argv: nil, refuses: []string{"needs a key", "pinchtab storage clear"}},
		{name: "empty --key is refused like a bare delete", argv: []string{"--key", ""}, refuses: []string{"pinchtab storage clear"}},
		bothGivenRow("key"),
		{name: "two keys", argv: []string{"k1", "k2"}, refuses: []string{"at most 1"}},
	})
}
