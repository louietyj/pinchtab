package external

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pinchtab/pinchtab/internal/autosolver"
)

func newTestTaskAPI(url string) *taskAPI {
	return &taskAPI{label: "test", baseURL: url, apiKey: "k", pollInterval: time.Millisecond, client: http.DefaultClient}
}

func TestTaskAPIReturnsASynchronousSolutionWithoutPolling(t *testing.T) {
	polled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/getTaskResult" {
			polled = true
		}
		_, _ = w.Write([]byte(`{"errorId":0,"status":"ready","taskId":"t","solution":{"distance":142}}`))
	}))
	defer srv.Close()

	raw, err := newTestTaskAPI(srv.URL).solve(context.Background(), map[string]string{"type": "VisionEngine"})
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if polled {
		t.Error("polled getTaskResult for a solution createTask already returned")
	}
	if string(raw) != `{"distance":142}` {
		t.Errorf("solution = %s", raw)
	}
}

func TestTaskAPIClassifiesCreateTaskRefusals(t *testing.T) {
	for code, permanent := range map[string]bool{
		"ERROR_INVALID_TASK_DATA":   true,
		"ERROR_ZERO_BALANCE":        true,
		"ERROR_KEY_DENIED_ACCESS":   true,
		"ERROR_RATE_LIMIT":          false,
		"ERROR_SERVICE_UNAVALIABLE": false,
		"ERROR_NO_SLOT_AVAILABLE":   false,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// CapSolver sends refusals as 400s; the body is what classifies them.
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errorId":1,"errorCode":"` + code + `"}`))
		}))
		_, err := newTestTaskAPI(srv.URL).solve(context.Background(), map[string]string{})
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), code) {
			t.Fatalf("%s: err = %v", code, err)
		}
		if got := errors.Is(err, autosolver.ErrPermanent); got != permanent {
			t.Errorf("%s: permanent = %v, want %v", code, got, permanent)
		}
	}
}

// A task still being worked when the deadline hits is billed when it finishes.
func TestTaskAPITimeoutWhilePollingIsSpent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/createTask" {
			_, _ = w.Write([]byte(`{"errorId":0,"taskId":"t"}`))
			return
		}
		_, _ = w.Write([]byte(`{"errorId":0,"status":"processing"}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := newTestTaskAPI(srv.URL).solve(ctx, map[string]string{})
	if !errors.Is(err, autosolver.ErrSpent) {
		t.Errorf("err = %v, want it marked spent", err)
	}
}

// 2Captcha numbers its task IDs; they must go back unquoted or it rejects the poll.
func TestTaskAPIEchoesANumericTaskIDVerbatim(t *testing.T) {
	var gotTaskID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/createTask" {
			_, _ = w.Write([]byte(`{"errorId":0,"taskId":83925302505}`))
			return
		}
		var req map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotTaskID = string(req["taskId"])
		_, _ = w.Write([]byte(`{"errorId":0,"status":"ready","solution":{"token":"tok"}}`))
	}))
	defer srv.Close()

	raw, err := newTestTaskAPI(srv.URL).solve(context.Background(), map[string]string{})
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if gotTaskID != "83925302505" {
		t.Errorf("polled with taskId %s, want the bare number", gotTaskID)
	}
	if !strings.Contains(string(raw), "tok") {
		t.Errorf("solution = %s", raw)
	}
}
