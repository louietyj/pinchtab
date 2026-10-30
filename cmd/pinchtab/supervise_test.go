package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitStepQuotesExpandsAndRedirects(t *testing.T) {
	env := map[string]string{"CRED_USERNAME": "me@x.com", "EMPTY": ""}
	getenv := func(k string) string { return env[k] }
	cases := []struct {
		line     string
		words    []string
		redirect string
	}{
		{`click e4`, []string{"click", "e4"}, ""},
		{`keyboard type "Glasg"`, []string{"keyboard", "type", "Glasg"}, ""},
		{`fill "#email" "$CRED_USERNAME"`, []string{"fill", "#email", "me@x.com"}, ""},
		{`fill #email ${CRED_USERNAME}x`, []string{"fill", "#email", "me@x.comx"}, ""},
		{`fill '#p' '$CRED_USERNAME'`, []string{"fill", "#p", "$CRED_USERNAME"}, ""},
		{`eval 'document.querySelectorAll("input").length'`, []string{"eval", `document.querySelectorAll("input").length`}, ""},
		{`type e3 "say \"hi\""`, []string{"type", "e3", `say "hi"`}, ""},
		{`click --x 134 --y 74 --unscale last-small`, []string{"click", "--x", "134", "--y", "74", "--unscale", "last-small"}, ""},
		{`text > /tmp/out.txt`, []string{"text"}, "/tmp/out.txt"},
		{`text >/tmp/out.txt`, []string{"text"}, "/tmp/out.txt"},
		{`fill e1 "a > b"`, []string{"fill", "e1", "a > b"}, ""},
		{`fill e1 ""`, []string{"fill", "e1", ""}, ""},
		{`price $5`, []string{"price", "$5"}, ""},
	}
	for _, c := range cases {
		words, redirect, err := splitStep(c.line, getenv)
		if err != nil {
			t.Errorf("%s: %v", c.line, err)
			continue
		}
		if !reflect.DeepEqual(words, c.words) || redirect != c.redirect {
			t.Errorf("%s: got %q > %q, want %q > %q", c.line, words, redirect, c.words, c.redirect)
		}
	}
	for _, bad := range []string{`fill e1 "open`, `fill e1 'open`, `text >`, `text > a > b`} {
		if _, _, err := splitStep(bad, getenv); err == nil {
			t.Errorf("%s: want an error", bad)
		}
	}
}

func shotPathsIn(t *testing.T) shotPaths {
	dir := t.TempDir()
	return shotPaths{name: "pinchtab", dir: dir, ledger: filepath.Join(dir, "ledger"), counter: filepath.Join(dir, "count")}
}

func TestApplyUnscaleDividesByTheShotsScale(t *testing.T) {
	p := shotPathsIn(t)
	if err := os.WriteFile(filepath.Join(p.dir, "latest.scale"), []byte("full 0.8\nsmall 0.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(inBatchEnv, "")
	got, used, err := applyUnscale([]string{"--x", "134", "--y=74", "--unscale", "last-small"}, p)
	if err != nil || !used {
		t.Fatalf("got %v %v", used, err)
	}
	if want := []string{"--x", "335", "--y", "185"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	got, _, err = applyUnscale([]string{"--x", "100", "--unscale=0.5"}, p)
	if err != nil || !reflect.DeepEqual(got, []string{"--x", "200"}) {
		t.Fatalf("explicit scale: got %q %v", got, err)
	}
	if _, used, _ := applyUnscale([]string{"e4"}, p); used {
		t.Fatal("no --unscale must leave the args alone")
	}
}

func TestApplyUnscaleInABatchUsesTheShotBeforeIt(t *testing.T) {
	p := shotPathsIn(t)
	if err := os.WriteFile(filepath.Join(p.dir, "latest.scale"), []byte("full 0.8\nsmall 0.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(inBatchEnv, "1")
	t.Setenv(batchScalesEnv, "")
	if _, _, err := applyUnscale([]string{"--x", "1", "--unscale", "last-full"}, p); err == nil || !strings.Contains(err.Error(), "no screenshot existed") {
		t.Fatalf("a batch begun before any shot must refuse, got %v", err)
	}
	t.Setenv(batchScalesEnv, "full 0.5\nsmall 0.25\n")
	if _, _, err := applyUnscale([]string{"--x", "1", "--unscale", "last-full"}, p); err == nil || !strings.Contains(err.Error(), "changed size") {
		t.Fatalf("a resized window must refuse, got %v", err)
	}
	t.Setenv(batchScalesEnv, "full 0.8\nsmall 0.4\n")
	got, _, err := applyUnscale([]string{"--x", "40", "--unscale", "last-small"}, p)
	if err != nil || !reflect.DeepEqual(got, []string{"--x", "100"}) {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestShouldShootOnlyPageChangingCommands(t *testing.T) {
	for _, c := range []struct {
		name string
		rest []string
		want bool
	}{
		{"click", []string{"e4"}, true},
		{"nav", []string{"https://x"}, true},
		{"text", nil, false},
		{"snap", nil, false},
		{"expect", []string{"--text", "x"}, false},
		{"wait", []string{"--text", "x"}, true},
		{"tab", nil, false},
		{"tab", []string{"ABC"}, true},
		{"tab", []string{"close", "ABC"}, false},
		{"set", []string{"viewport", "1x1"}, true},
		{"set", []string{"geo"}, false},
		{"clipboard", []string{"paste"}, true},
	} {
		if got := shouldShoot(c.name, c.rest); got != c.want {
			t.Errorf("%s %v: got %v", c.name, c.rest, got)
		}
	}
}

func TestSplitCommandSeparatesRootFlags(t *testing.T) {
	g, name, rest := splitCommand([]string{"--server", "http://x", "click", "e4", "--snap"})
	if !reflect.DeepEqual(g, []string{"--server", "http://x"}) || name != "click" || !reflect.DeepEqual(rest, []string{"e4", "--snap"}) {
		t.Fatalf("got %q %q %q", g, name, rest)
	}
	if !isBrowserCommand("click") || !isBrowserCommand("expect") || isBrowserCommand("server") {
		t.Fatal("browser command set is wrong")
	}
}

func TestShotPathsMatchTheTier(t *testing.T) {
	t.Setenv("HOME", "/h")
	t.Setenv("PINCHTAB_SHOT_DIR", "")
	t.Setenv("PINCHTAB_NAME", "pinchtab-windows")
	t.Setenv("PINCHTAB_AGENT", "sub1")
	p := currentShotPaths()
	tmp := os.TempDir()
	want := shotPaths{
		name:    "pinchtab-windows",
		dir:     "/h/pinchtab-windows-shots/sub1",
		ledger:  filepath.Join(tmp, "pinchtab-windows-shots-unseen-sub1"),
		counter: filepath.Join(tmp, "pinchtab-windows-shot-count-sub1"),
	}
	if p != want {
		t.Fatalf("got %+v, want %+v", p, want)
	}
}

func TestListShotsShowsTheLastTenAndClears(t *testing.T) {
	p := shotPathsIn(t)
	var w bytes.Buffer
	listShots(p, &w)
	if !strings.Contains(w.String(), "none since") {
		t.Fatalf("empty ledger: %q", w.String())
	}
	for i := 1; i <= 12; i++ {
		appendLine(p.ledger, "shot"+string(rune('a'+i)))
	}
	w.Reset()
	listShots(p, &w)
	if !strings.Contains(w.String(), "2 older not shown") || !strings.Contains(w.String(), "shotm") {
		t.Fatalf("got %q", w.String())
	}
	w.Reset()
	listShots(p, &w)
	if !strings.Contains(w.String(), "none since") {
		t.Fatalf("not cleared: %q", w.String())
	}
}

func TestGrepFiltersOutputButKeepsTheSnapHeader(t *testing.T) {
	rest, re, err := cutGrep([]string{"--grep", "sort|filter", "-s", "main"})
	if err != nil || !reflect.DeepEqual(rest, []string{"-s", "main"}) {
		t.Fatalf("got %q %v", rest, err)
	}
	got := string(grepLines([]byte("# Desks | https://x | 9 nodes\ne1:button \"Sort\"\ne2:link \"Home\"\ne3:button \"All filters\"\n"), re))
	if got != "# Desks | https://x | 9 nodes\ne1:button \"Sort\"\ne3:button \"All filters\"\n" {
		t.Fatalf("got %q", got)
	}
	if rest, re, _ := cutGrep([]string{"e4"}); re != nil || !reflect.DeepEqual(rest, []string{"e4"}) {
		t.Fatal("no --grep must leave the args alone")
	}
	if _, _, err := cutGrep([]string{"--grep", "("}); err == nil {
		t.Fatal("a bad pattern must be refused")
	}
}

func TestHeadWithRestCutsLongOutputToAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "failed-snap.txt")
	short := []byte("# Page | https://x | 2 nodes\ne1:button \"Go\"\n")
	if got := headWithRest(short, path); !bytes.Equal(got, short) {
		t.Fatalf("short output must pass through, got %q", got)
	}
	var long bytes.Buffer
	for i := 0; i < 100; i++ {
		long.WriteString("line\n")
	}
	got := string(headWithRest(long.Bytes(), path))
	if strings.Count(got, "line\n") != failedOutputLines || !strings.Contains(got, "60 more lines in "+path) {
		t.Fatalf("got %q", got)
	}
	if saved, _ := os.ReadFile(path); !bytes.Equal(saved, long.Bytes()) {
		t.Fatal("the whole output must be saved")
	}
}

func TestQuoteHintRebuildsTheStep(t *testing.T) {
	if got := quoteHint([]string{"click", "text:Second", "section", "--snap"}); got != `quote a selector that has spaces: click "text:Second section" --snap` {
		t.Fatalf("got %q", got)
	}
	if got := quoteHint([]string{"fill", "e1", "a", "b"}); !strings.Contains(got, "e.g.") {
		t.Fatalf("got %q", got)
	}
}

func TestQuietOutputDropsWhatTheStatusLineSays(t *testing.T) {
	got := string(quietOutput([]byte("OK\n0123456789ABCDEF0123456789ABCDEF\nOK navigated https://x\n")))
	if got != "OK navigated https://x\n" {
		t.Fatalf("got %q", got)
	}
}
