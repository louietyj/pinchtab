package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pinchtab/pinchtab/internal/config"
)

func TestResolveCLIBase(t *testing.T) {
	tests := []struct {
		name       string
		serverFlag string
		envURL     string
		expected   string
	}{
		{
			name:       "--server overrides everything",
			serverFlag: "http://remote:1234",
			envURL:     "http://env:5678",
			expected:   "http://remote:1234",
		},
		{
			name:       "--server trims trailing slash",
			serverFlag: "http://remote:1234/",
			expected:   "http://remote:1234",
		},
		{
			name:     "PINCHTAB_SERVER overrides fallback",
			envURL:   "http://env:5678",
			expected: "http://env:5678",
		},
		{
			name:     "PINCHTAB_SERVER trims trailing slash",
			envURL:   "http://env:5678/",
			expected: "http://env:5678",
		},
		{
			name:     "default fallback uses 127.0.0.1 and server port",
			expected: "http://127.0.0.1:9867",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldServerURL := serverURL
			serverURL = tt.serverFlag
			defer func() { serverURL = oldServerURL }()

			if tt.envURL != "" {
				t.Setenv("PINCHTAB_SERVER", tt.envURL)
			} else {
				t.Setenv("PINCHTAB_SERVER", "")
			}

			cfg := &config.RuntimeConfig{Port: "9867"}

			actual := resolveCLIBase(cfg)
			if actual != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, actual)
			}
		})
	}
}

func TestCanAutoStartServerForCLIOnlyAllowsDefaultLocalBase(t *testing.T) {
	oldServerURL := serverURL
	defer func() { serverURL = oldServerURL }()

	cfg := &config.RuntimeConfig{Port: "9867"}
	serverURL = ""
	t.Setenv("PINCHTAB_SERVER", "")

	if !canAutoStartServerForCLI(cfg, "http://127.0.0.1:9867") {
		t.Fatal("expected default local base to allow auto-start")
	}
	if canAutoStartServerForCLI(cfg, "http://127.0.0.1:9999") {
		t.Fatal("expected mismatched base to disable auto-start")
	}

	serverURL = "http://127.0.0.1:9999"
	if canAutoStartServerForCLI(cfg, "http://127.0.0.1:9999") {
		t.Fatal("expected explicit --server target to disable auto-start")
	}

	serverURL = ""
	t.Setenv("PINCHTAB_SERVER", "http://127.0.0.1:9999")
	if canAutoStartServerForCLI(cfg, "http://127.0.0.1:9999") {
		t.Fatal("expected PINCHTAB_SERVER target to disable auto-start")
	}
}

func TestResolveCLIAgentID(t *testing.T) {
	tests := []struct {
		name      string
		flagValue string
		envValue  string
		expected  string
	}{
		{
			name:      "--agent-id overrides environment",
			flagValue: "agent-flag",
			envValue:  "agent-env",
			expected:  "agent-flag",
		},
		{
			name:      "--agent-id trims whitespace",
			flagValue: "  agent-flag  ",
			expected:  "agent-flag",
		},
		{
			name:      "blank --agent-id falls through to environment",
			flagValue: "   ",
			envValue:  "agent-env",
			expected:  "agent-env",
		},
		{
			name:     "PINCHTAB_AGENT_ID overrides default",
			envValue: "agent-env",
			expected: "agent-env",
		},
		{
			name:     "PINCHTAB_AGENT_ID trims whitespace",
			envValue: "  agent-env  ",
			expected: "agent-env",
		},
		{
			name:      "blank values fall back to empty (anonymous)",
			flagValue: "   ",
			envValue:  "   ",
			expected:  "",
		},
		{
			name:     "default fallback is empty (anonymous)",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldAgentID := cliAgentID
			cliAgentID = tt.flagValue
			defer func() { cliAgentID = oldAgentID }()

			t.Setenv("PINCHTAB_AGENT_ID", tt.envValue)

			if got := resolveCLIAgentID(); got != tt.expected {
				t.Fatalf("resolveCLIAgentID() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestRunCLIWithInjectsAgentIDHeaders(t *testing.T) {
	const wantAgentID = "agent-main"

	var gotRequest *http.Request
	client := &http.Client{
		Transport: agentHeaderTransport{
			base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				gotRequest = req
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(http.NoBody),
					Request:    req,
				}, nil
			}),
			agentID: wantAgentID,
		},
	}

	req, err := http.NewRequest(http.MethodGet, "http://example.test/health", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if _, err := client.Do(req); err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}

	if gotRequest == nil {
		t.Fatal("transport did not receive request")
	}
	if got := gotRequest.Header.Get("X-Agent-Id"); got != wantAgentID {
		t.Fatalf("X-Agent-Id = %q, want %q", got, wantAgentID)
	}
}

func TestCLITransportAddsPinchtabHeaders(t *testing.T) {
	t.Setenv("PINCHTAB_HEADERS", "CF-Access-Client-Id: id.access\nCF-Access-Client-Secret: secret")

	var gotRequest *http.Request
	client := &http.Client{
		Transport: agentHeaderTransport{
			base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				gotRequest = req
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(http.NoBody), Request: req}, nil
			}),
		},
	}
	req, _ := http.NewRequest(http.MethodGet, "https://browser.example/health", nil)
	if _, err := client.Do(req); err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	if got := gotRequest.Header.Get("Cf-Access-Client-Id"); got != "id.access" {
		t.Errorf("Cf-Access-Client-Id = %q", got)
	}
	if got := gotRequest.Header.Get("Cf-Access-Client-Secret"); got != "secret" {
		t.Errorf("Cf-Access-Client-Secret = %q", got)
	}
}

// Probing this unreachable host would fail with "not running".
func TestEnsureServerForCLISkipsRemoteServers(t *testing.T) {
	if err := ensureServerForCLI(&config.RuntimeConfig{Port: "9867"}, "https://browser.example.invalid", "", "nav"); err != nil {
		t.Fatalf("ensureServerForCLI(remote) = %v, want nil", err)
	}
}

// TestPreflightBrowserBinary covers the fail-fast no-browser diagnosis that
// replaces the bridge's opaque "instance not ready after 10s" 503 on the
// documented `pinchtab nav` cold start.
func TestPreflightBrowserBinary(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "chrome")
	if err := os.WriteFile(existing, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("missing override fails fast with guidance", func(t *testing.T) {
		err := preflightBrowserBinary(&config.RuntimeConfig{
			DefaultBrowser: "chrome",
			BrowserBinary:  filepath.Join(t.TempDir(), "does-not-exist"),
		})
		if err == nil {
			t.Fatal("expected error for a missing browser.binary override")
		}
		if !strings.Contains(err.Error(), "browser executable") || !strings.Contains(err.Error(), "doctor") {
			t.Errorf("error should describe the configured browser executable and point at doctor; got: %v", err)
		}
	})

	t.Run("existing override passes", func(t *testing.T) {
		if err := preflightBrowserBinary(&config.RuntimeConfig{
			DefaultBrowser: "chrome",
			BrowserBinary:  existing,
		}); err != nil {
			t.Errorf("expected nil for an existing override binary; got %v", err)
		}
	})

	t.Run("default target binary passes", func(t *testing.T) {
		if err := preflightBrowserBinary(&config.RuntimeConfig{
			DefaultBrowser: config.BrowserChrome,
			Targets: config.BrowserTargetsConfig{
				"only": {
					Provider: config.BrowserCloak,
					Binary:   existing,
				},
			},
		}); err != nil {
			t.Errorf("expected nil for an existing default-target binary; got %v", err)
		}
	})

	t.Run("external CDP attach skips local binary check", func(t *testing.T) {
		if err := preflightBrowserBinary(&config.RuntimeConfig{
			DefaultBrowser: "chrome",
			CDPAttachURL:   "http://127.0.0.1:9222",
		}); err != nil {
			t.Errorf("expected nil when attaching to external CDP; got %v", err)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestAnInvalidServerBaseExitsTwoNamingItsSource(t *testing.T) {
	if args := os.Getenv("PINCHTAB_BAD_BASE_ARGS"); args != "" {
		rootCmd.SetArgs(strings.Split(args, "\x1f"))
		if err := rootCmd.Execute(); err != nil {
			os.Exit(commandExitCode(err))
		}
		return
	}

	for _, tc := range []struct {
		name, env, source, value string
		args                     []string
	}{
		{"env without a scheme", "127.0.0.1:9867", "PINCHTAB_SERVER", "127.0.0.1:9867", []string{"tab"}},
		{"env host read as a scheme", "localhost:9867", "PINCHTAB_SERVER", "localhost:9867", []string{"tab"}},
		{"flag with a space in the host", "", "--server", "http://bad host:9867", []string{"--server", "http://bad host:9867", "tab"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := exec.Command(os.Args[0], "-test.run=^TestAnInvalidServerBaseExitsTwoNamingItsSource$", "-test.timeout=30s") // #nosec G204 -- re-executes this test binary with fixed arguments.
			child.Env = append(os.Environ(),
				"PINCHTAB_BAD_BASE_ARGS="+strings.Join(tc.args, "\x1f"),
				"PINCHTAB_SERVER="+tc.env,
				"PINCHTAB_TOKEN=x",
				"HOME="+t.TempDir(),
				"XDG_STATE_HOME="+t.TempDir(),
			)
			var stderr bytes.Buffer
			child.Stderr = &stderr
			err := child.Run()

			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
				t.Fatalf("exit = %v, want 2; stderr:\n%s", err, stderr.String())
			}
			out := stderr.String()
			if strings.Contains(out, "panic:") {
				t.Fatalf("the CLI panicked:\n%s", out)
			}
			if !strings.Contains(out, tc.source) || !strings.Contains(out, `"`+tc.value+`"`) {
				t.Fatalf("stderr does not name %s and %q:\n%s", tc.source, tc.value, out)
			}
		})
	}
}

func TestValidServerBasesStillResolve(t *testing.T) {
	oldServerURL := serverURL
	t.Cleanup(func() { serverURL = oldServerURL })
	for value, want := range map[string]string{
		"http://127.0.0.1:9867":  "http://127.0.0.1:9867",
		"https://host":           "https://host",
		"http://127.0.0.1:9867/": "http://127.0.0.1:9867",
	} {
		for _, viaFlag := range []bool{true, false} {
			serverURL = ""
			t.Setenv("PINCHTAB_SERVER", "")
			if viaFlag {
				serverURL = value
			} else {
				t.Setenv("PINCHTAB_SERVER", value)
			}
			got, err := resolveBaseURL("http://127.0.0.1:9999")
			if err != nil || got != want {
				t.Errorf("%q (flag=%v) resolved to %q, %v; want %q", value, viaFlag, got, err, want)
			}
		}
	}
}
