package apiclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A step the server held back for a solve is sent again once the solve is
// over, so the caller sees one blocking call.
func TestAStepHeldForASolveIsSentAgainAfterIt(t *testing.T) {
	var clicks, waits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/action":
			if clicks.Add(1) == 1 {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"code":"solve_in_progress","details":{"tabId":"t1"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true}`))
		case "/autosolve":
			if r.URL.Query().Get("tabId") != "t1" {
				t.Errorf("waited on tab %q", r.URL.Query().Get("tabId"))
			}
			state := "solving"
			if waits.Add(1) > 1 {
				state = "done"
			}
			_, _ = w.Write([]byte(`{"state":"` + state + `"}`))
		}
	}))
	defer srv.Close()

	status, body, err := doRequest(srv.Client(), "", newRequest(http.MethodPost, srv.URL, "/action", WithBody(map[string]any{"kind": "click"})))
	if err != nil || status != http.StatusOK || string(body) != `{"success":true}` {
		t.Fatalf("got %d %s %v", status, body, err)
	}
	if clicks.Load() != 2 || waits.Load() != 2 {
		t.Errorf("clicks = %d, waits = %d; want the click sent twice around two waits", clicks.Load(), waits.Load())
	}
}

// A step whose own solve outlasted the request reports the solve's outcome,
// not "still being solved".
func TestAPendingSolveIsWaitedOutAndReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/navigate":
			_, _ = w.Write([]byte(`{"tabId":"t1","autoSolve":{"solved":false,"pending":true,"tabId":"t1"}}`))
		case "/autosolve":
			_, _ = w.Write([]byte(`{"state":"done","autoSolve":{"solved":true,"solver":"twocaptcha","challengeType":"hcaptcha"}}`))
		}
	}))
	defer srv.Close()

	_, body, err := doRequest(srv.Client(), "", newRequest(http.MethodPost, srv.URL, "/navigate"))
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		AutoSolve map[string]any `json:"autoSolve"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.AutoSolve["solved"] != true || resp.AutoSolve["pending"] != nil {
		t.Fatalf("body = %s", body)
	}
}

func TestAwaitCaptchaLingersOnceAndReportsTheSolve(t *testing.T) {
	var lingers atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("lingerMs") != "" {
			lingers.Add(1)
			_, _ = w.Write([]byte(`{"state":"solving","challengeType":"hcaptcha"}`))
			return
		}
		_, _ = w.Write([]byte(`{"state":"done","autoSolve":{"solved":true}}`))
	}))
	defer srv.Close()

	out := AwaitCaptcha(srv.Client(), srv.URL, "", 3*time.Second)
	if out["solved"] != true || lingers.Load() != 1 {
		t.Fatalf("out = %v, lingers = %d", out, lingers.Load())
	}
	idle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"state":"idle"}`))
	}))
	defer idle.Close()
	if out := AwaitCaptcha(idle.Client(), idle.URL, "", time.Second); out != nil {
		t.Errorf("idle tab reported %v", out)
	}
}
