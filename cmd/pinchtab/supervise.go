package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/pinchtab/pinchtab/internal/cli/clistate"
)

// The supervisor runs a browser command as a child of this binary so it can act
// after the command, whatever way the command exits (most failures os.Exit):
// save a screenshot, retry once on a dead session. execute-batch runs each step
// through it the same way.
const (
	supervisedEnv  = "PINCHTAB_SUPERVISED"   // set on the child that runs the command itself
	batchScalesEnv = "PINCHTAB_BATCH_SCALES" // latest.scale as it was when the batch began
	inBatchEnv     = "PINCHTAB_IN_BATCH"
)

// cliSettings is the CLI's own settings file, cli.json beside config.json. The
// server never reads it.
type cliSettings struct {
	// Autoshot saves a screenshot after every command that can change the page.
	Autoshot bool `json:"autoshot"`
	// StickySession creates an agent session on first use and reuses it across
	// invocations, so callers never export PINCHTAB_SESSION.
	StickySession bool `json:"stickySession"`
}

func cliSettingsPath() string {
	return filepath.Join(filepath.Dir(cliTokenConfigPath()), "cli.json")
}

func loadCLISettings() cliSettings {
	var s cliSettings
	if data, err := os.ReadFile(cliSettingsPath()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	if v := os.Getenv("PINCHTAB_AUTOSHOT"); v != "" {
		s.Autoshot = v == "1"
	}
	return s
}

// shotPaths names where one tier's screenshots go. The name is the command the
// agent types (pinchtab, pinchtab-windows, pinchtab-cloud), so tiers and agents
// never share a counter or a latest.jpg.
type shotPaths struct {
	name    string
	dir     string
	ledger  string
	counter string
}

func currentShotPaths() shotPaths {
	name := strings.TrimSpace(os.Getenv("PINCHTAB_NAME"))
	if name == "" {
		name = "pinchtab"
	}
	base := os.Getenv("PINCHTAB_SHOT_DIR")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, name+"-shots")
	}
	p := shotPaths{
		name:    name,
		dir:     base,
		ledger:  filepath.Join(os.TempDir(), name+"-shots-unseen"),
		counter: filepath.Join(os.TempDir(), name+"-shot-count"),
	}
	if agent := os.Getenv("PINCHTAB_AGENT"); agent != "" {
		p.dir = filepath.Join(p.dir, agent)
		p.ledger += "-" + agent
		p.counter += "-" + agent
	}
	return p
}

func (p shotPaths) count() int {
	data, _ := os.ReadFile(p.counter)
	n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return n
}

// superviseCommand handles execute-batch and shots, and runs a browser command
// under supervision when a setting, --unscale or --grep calls for it.
// handled=false means the command runs in this process as usual.
func superviseCommand(args []string) (code int, handled bool) {
	if os.Getenv(supervisedEnv) == "1" {
		return 0, false
	}
	globals, name, rest := splitCommand(args)
	switch name {
	case "execute-batch":
		return runBatch(globals, rest, os.Stdin, os.Stdout), true
	case "shots":
		return listShots(currentShotPaths(), os.Stdout), true
	}
	if !isBrowserCommand(name) || helpRequested(rest) {
		return 0, false
	}
	settings := loadCLISettings()
	paths := currentShotPaths()
	rest, unscaled, err := applyUnscale(rest, paths)
	var grep *regexp.Regexp
	if err == nil {
		rest, grep, err = cutGrep(rest)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", paths.name, err)
		return 2, true
	}
	sticky := settings.StickySession && stickySessionApplies(globals)
	if !settings.Autoshot && !sticky && !unscaled && grep == nil {
		return 0, false
	}
	cmdArgs := append(append(append([]string{}, globals...), name), rest...)
	return superviseOnce(cmdArgs, name, rest, settings.Autoshot, sticky, grep, paths), true
}

// cutGrep takes `--grep <regex>` off a command: its output is filtered to the
// matching lines (case-insensitive), so `snap --grep "sort|filter"` reads just
// the refs wanted instead of piping a whole tree through grep.
func cutGrep(args []string) ([]string, *regexp.Regexp, error) {
	out := make([]string, 0, len(args))
	var pattern string
	found := false
	for i := 0; i < len(args); i++ {
		if v, ok := strings.CutPrefix(args[i], "--grep="); ok {
			pattern, found = v, true
			continue
		}
		if args[i] == "--grep" {
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("--grep needs a pattern")
			}
			pattern, found = args[i+1], true
			i++
			continue
		}
		out = append(out, args[i])
	}
	if !found {
		return args, nil, nil
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, nil, fmt.Errorf("--grep: %v", err)
	}
	return out, re, nil
}

// grepLines keeps the lines re matches, and a snapshot's `# title | url` header
// so a filtered snap still says which page it read.
func grepLines(out []byte, re *regexp.Regexp) []byte {
	var b bytes.Buffer
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if strings.HasPrefix(line, "# ") || re.MatchString(line) {
			b.WriteString(line + "\n")
		}
	}
	return b.Bytes()
}

func superviseOnce(cmdArgs []string, name string, rest []string, autoshot, sticky bool, grep *regexp.Regexp, paths shotPaths) int {
	env := os.Environ()
	if sticky {
		env = withSession(env, false)
	}
	stdout, stderr, code := runSelf(cmdArgs, env, nil)
	if sticky && bytes.Contains(stderr, []byte("bad_session")) {
		env = withSession(os.Environ(), true)
		stdout, stderr, code = runSelf(cmdArgs, env, nil)
	}
	// Failures get a shot too: "why didn't that click land?" is answered by looking.
	if autoshot && shouldShoot(name, rest) {
		takeShot(paths, name, tabFlag(rest), env)
	}
	if grep != nil && code == 0 {
		stdout = grepLines(stdout, grep)
	}
	_, _ = os.Stdout.Write(stdout)
	_, _ = os.Stderr.Write(stderr)
	return code
}

// runSelf runs this binary with args, unsupervised, and returns what it printed.
func runSelf(args, env []string, stdin io.Reader) ([]byte, []byte, int) {
	return runBinary(args, append(withoutEnv(env, supervisedEnv), supervisedEnv+"=1"), stdin)
}

// runSupervised runs a step as the agent would type it: through the supervisor,
// so it gets the session, --unscale and its screenshot.
func runSupervised(args, env []string) ([]byte, []byte, int) {
	return runBinary(args, withoutEnv(env, supervisedEnv), nil)
}

func runBinary(args, env []string, stdin io.Reader) ([]byte, []byte, int) {
	self, err := os.Executable()
	if err != nil {
		return nil, []byte(err.Error() + "\n"), 1
	}
	cmd := exec.Command(self, args...)
	cmd.Env = env
	cmd.Stdin = stdin
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		errOut.WriteString(err.Error() + "\n")
		code = 1
	}
	return out.Bytes(), errOut.Bytes(), code
}

// splitCommand separates the root's own flags from the command and its args.
func splitCommand(args []string) (globals []string, name string, rest []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--server" || a == "--agent-id":
			globals = append(globals, args[i:min(i+2, len(args))]...)
			i++
		case strings.HasPrefix(a, "--server=") || strings.HasPrefix(a, "--agent-id="):
			globals = append(globals, a)
		default:
			return globals, a, args[i+1:]
		}
	}
	return globals, "", nil
}

func isBrowserCommand(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range browserRootCommands() {
		if c.Name() == name || c.HasAlias(name) {
			return true
		}
	}
	return false
}

func helpRequested(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

func stickySessionApplies(globals []string) bool {
	if os.Getenv("PINCHTAB_SERVER") != "" {
		return false
	}
	for _, g := range globals {
		if g == "--server" || strings.HasPrefix(g, "--server=") {
			return false
		}
	}
	return true
}

func sessionFile() string {
	name := "session"
	if agent := os.Getenv("PINCHTAB_AGENT"); agent != "" {
		name += "-" + agent
	}
	return filepath.Join(clistate.Dir(), name)
}

// withSession returns env carrying the sticky session, creating one when none
// is saved (or fresh is set). A session the caller exported is adopted, so the
// next invocation, which has no export, lands on the same tab.
func withSession(env []string, fresh bool) []string {
	file := sessionFile()
	if s := os.Getenv("PINCHTAB_SESSION"); s != "" && !fresh {
		if cur, _ := os.ReadFile(file); string(cur) != s && strings.HasPrefix(s, "ses_") {
			_ = os.MkdirAll(filepath.Dir(file), 0o700)
			_ = os.WriteFile(file, []byte(s), 0o600)
		}
		return env
	}
	id := ""
	if !fresh {
		data, _ := os.ReadFile(file)
		id = strings.TrimSpace(string(data))
	}
	if id == "" {
		agent := os.Getenv("PINCHTAB_AGENT")
		if agent == "" {
			agent = "claude"
		}
		// session create must run unscoped: a session-scoped caller gets 403.
		out, _, _ := runSelf([]string{"session", "create", "--agent-id", agent}, withoutEnv(env, "PINCHTAB_SESSION"), nil)
		id = strings.TrimSpace(string(out))
		if !strings.HasPrefix(id, "ses_") {
			_ = os.Remove(file)
			return env
		}
		_ = os.MkdirAll(filepath.Dir(file), 0o700)
		_ = os.WriteFile(file, []byte(id), 0o600)
	}
	return append(withoutEnv(env, "PINCHTAB_SESSION"), "PINCHTAB_SESSION="+id)
}

func withoutEnv(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return out
}

// shouldShoot reports whether a command can change the page.
func shouldShoot(name string, rest []string) bool {
	first := ""
	if len(rest) > 0 {
		first = rest[0]
	}
	switch name {
	case "nav", "goto", "navigate", "open", "quick", "back", "forward", "reload",
		"click", "dblclick", "hover", "focus", "mouse", "drag",
		"fill", "type", "press", "keydown", "keyup", "keyboard", "select", "check", "uncheck", "upload",
		"scroll", "scrollintoview", "dialog", "vision",
		// Changes nothing itself, but returns once the page has: without a shot,
		// latest would be the page as it was before the wait.
		"wait":
		return true
	case "tab", "tabs":
		// `tab` alone lists tabs; `tab <id>` switches to one.
		return first != "" && !strings.HasPrefix(first, "-") &&
			first != "close" && first != "handoff" && first != "resume" && first != "handoff-status"
	case "clipboard":
		return first == "paste"
	case "set":
		return first == "viewport" || first == "media"
	}
	return false
}

func tabFlag(args []string) string {
	for i, a := range args {
		if a == "--tab" && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, "--tab="); ok {
			return v
		}
	}
	return ""
}

var imageLine = regexp.MustCompile(`^Image (\S+) (\d+x\d+)(?:, scale ([0-9.]+))?`)

// takeShot saves the page at two sizes after a command: numbered files kept for
// looking back, and fixed latest paths an agent can view in the same message as
// the command. Full is at most ~1.15MP, past which claude.ai shrinks images;
// small is a quarter of the tokens.
func takeShot(p shotPaths, name, tab string, env []string) {
	tabArgs := []string{}
	if tab != "" {
		tabArgs = []string{"--tab", tab}
	}
	// A shot taken the moment the command returns catches spinners and half-applied
	// re-renders, each costing the agent another look.
	runSelf(append([]string{"wait", "--dom-quiet"}, tabArgs...), env, nil)

	n := p.count() + 1
	_ = os.WriteFile(p.counter, []byte(strconv.Itoa(n)+"\n"), 0o644)
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return
	}
	shot := filepath.Join(p.dir, fmt.Sprintf("%04d-%s.jpg", n, name))
	small := strings.TrimSuffix(shot, ".jpg") + "-small.jpg"
	latest, latestSmall := filepath.Join(p.dir, "latest.jpg"), filepath.Join(p.dir, "latest-small.jpg")
	// Cleared first so a failed shot reads as missing, not as the previous page.
	_ = os.Remove(latest)
	_ = os.Remove(latestSmall)
	out, _, _ := runSelf(append([]string{"screenshot", "-o", shot, "--max-pixels", "1150000", "--also-max-pixels", "281000"}, tabArgs...), env, nil)
	if !nonEmpty(shot) {
		return
	}
	dims := map[string]string{}
	scales := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if m := imageLine.FindStringSubmatch(line); m != nil {
			d := m[2]
			if m[3] != "" {
				d += ", scale " + m[3]
				scales[m[1]] = m[3]
			}
			dims[m[1]] = d
		}
	}
	entry := fmt.Sprintf("%s (%s)", shot, orUnknown(dims[shot]))
	_ = copyFile(shot, latest)
	if nonEmpty(small) {
		entry += fmt.Sprintf(", small: %s (%s)", filepath.Base(small), orUnknown(dims[small]))
		_ = copyFile(small, latestSmall)
	}
	appendLine(p.ledger, entry)
	// Read by --unscale last-small|last-full.
	scaleFile := "full " + scales[shot] + "\n"
	if nonEmpty(small) {
		scaleFile += "small " + scales[small] + "\n"
	}
	_ = os.WriteFile(filepath.Join(p.dir, "latest.scale"), []byte(scaleFile), 0o644)
}

func orUnknown(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

func nonEmpty(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func appendLine(path, line string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}

// listShots prints the numbered shots since it last ran, for looking back past
// latest at an earlier step of a batch.
func listShots(p shotPaths, w io.Writer) int {
	data, _ := os.ReadFile(p.ledger)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if lines[0] == "" {
		fmt.Fprintf(w, "screenshots: none since the last `%s shots`\n", p.name)
		return 0
	}
	fmt.Fprintf(w, "screenshots since the last `%s shots`, newest last:\n", p.name)
	if len(lines) > 10 {
		fmt.Fprintf(w, "  ... %d older not shown, numbered just before the first below\n", len(lines)-10)
		lines = lines[len(lines)-10:]
	}
	for _, l := range lines {
		fmt.Fprintf(w, "  %s\n", l)
	}
	_ = os.WriteFile(p.ledger, nil, 0o644)
	return 0
}

func readScales(content string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			out[f[0]] = f[1]
		}
	}
	return out
}

// applyUnscale turns `--x X --y Y --unscale last-small|last-full|S` into CSS
// pixels: X,Y were read off the small or full image of a shot, and dividing by
// 0.4249 in an agent's head goes wrong. last-* means the newest shot before the
// batch (the one the agent read its points off), or outside a batch the newest.
func applyUnscale(args []string, p shotPaths) ([]string, bool, error) {
	norm := make([]string, 0, len(args))
	for _, a := range args {
		if v, ok := strings.CutPrefix(a, "--unscale="); ok {
			norm = append(norm, "--unscale", v)
			continue
		}
		norm = append(norm, a)
	}
	idx := -1
	for i, a := range norm {
		if a == "--unscale" {
			idx = i
		}
	}
	if idx < 0 {
		return args, false, nil
	}
	if idx+1 >= len(norm) {
		return nil, false, fmt.Errorf("--unscale takes last-small, last-full, or a scale such as 0.4249")
	}
	value := norm[idx+1]
	scale := value
	if size, ok := strings.CutPrefix(value, "last-"); ok && (size == "small" || size == "full") {
		now := readScales(readString(filepath.Join(p.dir, "latest.scale")))
		seen := now
		if os.Getenv(inBatchEnv) == "1" {
			seen = readScales(os.Getenv(batchScalesEnv))
		}
		scale = seen[size]
		if scale == "" {
			return nil, false, fmt.Errorf("--unscale %s: no screenshot existed before this; view one first", value)
		}
		// A changed scale is a resized window: the point read off the old image no
		// longer lands where it did, so refuse rather than click elsewhere.
		if cur := now[size]; cur != "" && cur != scale {
			img := "latest.jpg"
			if size == "small" {
				img = "latest-small.jpg"
			}
			return nil, false, fmt.Errorf("the window changed size since the screenshot you read (scale %s, now %s); view %s and read the point again", scale, cur, img)
		}
	}
	s, err := strconv.ParseFloat(scale, 64)
	if err != nil || s <= 0 {
		return nil, false, fmt.Errorf("--unscale takes last-small, last-full, or a scale such as 0.4249")
	}
	out := make([]string, 0, len(norm))
	sawXY := false
	for i := 0; i < len(norm); i++ {
		a := norm[i]
		if i == idx {
			i++
			continue
		}
		name, inline, hasInline := strings.Cut(a, "=")
		if (name == "--x" || name == "--y") && (hasInline || i+1 < len(norm)) {
			raw := inline
			if !hasInline {
				i++
				raw = norm[i]
			}
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return nil, false, fmt.Errorf("%s %q is not a number", name, raw)
			}
			out = append(out, name, strconv.Itoa(int(math.Floor(v/s+0.5))))
			sawXY = true
			continue
		}
		out = append(out, a)
	}
	if !sawXY {
		return nil, false, fmt.Errorf("--unscale needs --x and/or --y")
	}
	return out, true, nil
}

func readString(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}

// runBatch runs the steps on in, one command per line, until one fails. Each
// step prints one status line instead of its usual output.
func runBatch(globals, flags []string, in *os.File, w io.Writer) int {
	var wantSnap, wantDiff, wantText bool
	for _, f := range flags {
		switch f {
		case "--snap":
			wantSnap = true
		case "--snap-diff":
			wantDiff = true
		case "--text":
			wantText = true
		default:
			fmt.Fprintf(os.Stderr, "execute-batch: unknown flag %s (it takes --snap, --snap-diff and --text)\n", f)
			return 2
		}
	}
	if st, err := in.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprintln(os.Stderr, "execute-batch: give the steps on stdin, one per line, e.g. in a <<'EOF' heredoc")
		return 2
	}
	p := currentShotPaths()
	env := append(os.Environ(),
		inBatchEnv+"=1",
		batchScalesEnv+"="+readString(filepath.Join(p.dir, "latest.scale")))
	if loadCLISettings().StickySession && stickySessionApplies(globals) {
		env = withSession(env, false)
	}
	first := p.count() + 1

	steps := 0
	rc := 0
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		steps++
		out, failed := runStep(globals, line, p.name, env)
		if failed != 0 {
			fmt.Fprintf(w, "%d FAILED %s\n", steps, truncate(line, 70))
			writeIndented(w, out)
			fmt.Fprintf(w, "stopped: the steps after %d did not run\n", steps)
			rc = failed
			break
		}
		fmt.Fprintf(w, "%d OK %s\n", steps, truncate(line, 70))
		writeIndented(w, quietOutput(out))
	}
	if steps == 0 {
		fmt.Fprintln(os.Stderr, "execute-batch: no steps on stdin; give one command per line, e.g. in a <<'EOF' heredoc")
		return 2
	}
	if last := p.count(); last > first {
		fmt.Fprintf(w, "shots %04d-%04d in %s/; the last is latest-small.jpg\n", first, last, p.dir)
	} else if last == first {
		fmt.Fprintf(w, "shot %04d in %s/, also latest-small.jpg\n", first, p.dir)
	}
	// Where the batch ended, stopped or not.
	for _, end := range []struct {
		want bool
		args []string
	}{{wantText, []string{"text"}}, {wantDiff, []string{"snap", "--diff"}}, {wantSnap, []string{"snap"}}} {
		if end.want {
			out, errOut, _ := runSupervised(append(append([]string{}, globals...), end.args...), env)
			_, _ = w.Write(out)
			_, _ = os.Stderr.Write(errOut)
		}
	}
	return rc
}

func runStep(globals []string, line, name string, env []string) ([]byte, int) {
	words, redirect, err := splitStep(line, os.Getenv)
	if err != nil {
		return []byte(err.Error() + "\n"), 2
	}
	if len(words) > 0 && (words[0] == name || words[0] == "pinchtab") {
		words = words[1:]
	}
	if len(words) == 0 {
		return []byte("empty step\n"), 2
	}
	if words[0] == "execute-batch" {
		return []byte("a batch can't contain a batch\n"), 2
	}
	stdout, stderr, code := runSupervised(append(append([]string{}, globals...), words...), env)
	// A usage dump buries the one line that matters under forty.
	if before, _, found := bytes.Cut(stderr, []byte("\nUsage:\n")); found {
		stderr = append(before, '\n')
		if bytes.Contains(stderr, []byte("arg(s), received")) {
			stderr = append(stderr, []byte(quoteHint(words)+"\n")...)
		}
	}
	if redirect != "" && code == 0 {
		if err := os.WriteFile(redirect, stdout, 0o644); err != nil {
			return append([]byte(err.Error()+"\n"), stderr...), 1
		}
		stdout = []byte(fmt.Sprintf("wrote %d bytes to %s\n", len(stdout), redirect))
	}
	return append(stdout, stderr...), code
}

// quoteHint names the fix for a selector split at its spaces, the usual cause
// of a step getting more arguments than it takes.
func quoteHint(words []string) string {
	for i, w := range words[1:] {
		if !strings.Contains(w, ":") || strings.HasPrefix(w, "-") {
			continue
		}
		end := i + 2
		for end < len(words) && !strings.HasPrefix(words[end], "-") {
			end++
		}
		if end > i+2 {
			fixed := append(append(append([]string{}, words[:i+1]...), strconv.Quote(strings.Join(words[i+1:end], " "))), words[end:]...)
			return "quote a selector that has spaces: " + strings.Join(fixed, " ")
		}
	}
	return `quote an argument that has spaces, e.g. click "text:Sign in"`
}

var tabIDLine = regexp.MustCompile(`^[0-9A-F]{32}$`)

// quietOutput drops what the status line already says: a bare OK, and the tab
// id nav prints.
func quietOutput(out []byte) []byte {
	var b bytes.Buffer
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" || line == "OK" || tabIDLine.MatchString(line) {
			continue
		}
		b.WriteString(line + "\n")
	}
	return b.Bytes()
}

func writeIndented(w io.Writer, out []byte) {
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line != "" {
			fmt.Fprintf(w, "    %s\n", line)
		}
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// splitStep splits a batch line into words the way a POSIX shell would for
// quoting, and expands $VAR and ${VAR}, so steps read exactly as commands typed
// after `pinchtab`. Nothing else is expanded: no globs, no command substitution.
// A trailing unquoted `> file` sends the step's output to that file. Unlike a
// shell, # inside a line is literal, so `click #submit` works unquoted.
func splitStep(line string, getenv func(string) string) (words []string, redirect string, err error) {
	var cur strings.Builder
	inWord := false
	redirecting := false
	flush := func() {
		if !inWord {
			return
		}
		if redirecting && redirect == "" {
			redirect = cur.String()
		} else {
			words = append(words, cur.String())
		}
		cur.Reset()
		inWord = false
	}
	expand := func(r []rune, i int) int {
		// r[i] == '$'; returns the index of the last rune consumed.
		if i+1 < len(r) && r[i+1] == '{' {
			end := strings.IndexRune(string(r[i+2:]), '}')
			if end < 0 {
				cur.WriteRune('$')
				return i
			}
			cur.WriteString(getenv(string(r[i+2 : i+2+end])))
			return i + 2 + end
		}
		j := i + 1
		for j < len(r) && (r[j] == '_' || r[j] >= 'A' && r[j] <= 'Z' || r[j] >= 'a' && r[j] <= 'z' || j > i+1 && r[j] >= '0' && r[j] <= '9') {
			j++
		}
		if j == i+1 {
			cur.WriteRune('$')
			return i
		}
		cur.WriteString(getenv(string(r[i+1 : j])))
		return j - 1
	}
	r := []rune(line)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case c == ' ' || c == '\t':
			flush()
		case c == '>' && !inWord:
			if redirecting {
				return nil, "", fmt.Errorf("only one > redirect per step")
			}
			redirecting = true
		case c == '\'':
			inWord = true
			end := strings.IndexRune(string(r[i+1:]), '\'')
			if end < 0 {
				return nil, "", fmt.Errorf("unterminated ' quote")
			}
			cur.WriteString(string(r[i+1 : i+1+end]))
			i += 1 + end
		case c == '"':
			inWord = true
			i++
			for ; i < len(r) && r[i] != '"'; i++ {
				switch {
				case r[i] == '\\' && i+1 < len(r) && strings.ContainsRune("\"\\$`", r[i+1]):
					i++
					cur.WriteRune(r[i])
				case r[i] == '$':
					i = expand(r, i)
				default:
					cur.WriteRune(r[i])
				}
			}
			if i >= len(r) {
				return nil, "", fmt.Errorf("unterminated \" quote")
			}
		case c == '\\':
			inWord = true
			if i+1 < len(r) {
				i++
				cur.WriteRune(r[i])
			}
		case c == '$':
			inWord = true
			i = expand(r, i)
		default:
			inWord = true
			cur.WriteRune(c)
		}
	}
	flush()
	if redirecting && redirect == "" {
		return nil, "", fmt.Errorf("> needs a file name")
	}
	return words, redirect, nil
}
