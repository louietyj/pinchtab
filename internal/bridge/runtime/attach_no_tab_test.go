package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/pinchtab/pinchtab/internal/config"
)

// recordingCDP answers every CDP command with an empty result and records its method.
type recordingCDP struct {
	mu      sync.Mutex
	methods []string
}

func (rc *recordingCDP) serve(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			_, _ = fmt.Fprintf(w, `{"webSocketDebuggerUrl":"ws://%s/devtools/browser/b"}`, r.Host)
			return
		}
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			payload, op, err := wsutil.ReadClientData(conn)
			if err != nil {
				return
			}
			if op != ws.OpText {
				continue
			}
			var command struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(payload, &command) != nil {
				return
			}
			rc.mu.Lock()
			rc.methods = append(rc.methods, command.Method)
			rc.mu.Unlock()
			if wsutil.WriteServerMessage(conn, ws.OpText, []byte(fmt.Sprintf(`{"id":%d,"result":{}}`, command.ID))) != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func (rc *recordingCDP) sent(method string) bool {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	for _, m := range rc.methods {
		if m == method {
			return true
		}
	}
	return false
}

func TestAttachOpensNoTabInTheBrowser(t *testing.T) {
	for _, tc := range []struct {
		name   string
		attach func(wsURL string) (context.CancelFunc, context.CancelFunc, error)
	}{
		{"cdpAttachUrl", func(wsURL string) (context.CancelFunc, context.CancelFunc, error) {
			cfg := &config.RuntimeConfig{CDPAttachURL: wsURL, AttachAllowSchemes: []string{"ws"}}
			_, allocCancel, _, browserCancel, _, err := initBrowserFromExistingCDP(cfg, nil)
			return allocCancel, browserCancel, err
		}},
		{"remoteCdpUrl", func(wsURL string) (context.CancelFunc, context.CancelFunc, error) {
			cfg := &config.RuntimeConfig{AttachAllowSchemes: []string{"ws"}}
			_, allocCancel, _, browserCancel, _, err := InitRemoteCDP(context.Background(), cfg, wsURL)
			return allocCancel, browserCancel, err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc := &recordingCDP{}
			server := rc.serve(t)
			wsURL := "ws://" + strings.TrimPrefix(server.URL, "http://") + "/devtools/browser/b"

			allocCancel, browserCancel, err := tc.attach(wsURL)
			if err != nil {
				t.Fatalf("attach: %v", err)
			}
			browserCancel()
			allocCancel()

			for _, m := range []string{"Target.createTarget", "Target.attachToTarget"} {
				if rc.sent(m) {
					t.Errorf("attach sent %s: it opened a tab in a browser it does not own", m)
				}
			}
		})
	}
}
