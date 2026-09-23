package external

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
