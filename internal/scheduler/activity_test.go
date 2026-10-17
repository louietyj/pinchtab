package scheduler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pinchtab/pinchtab/internal/activity"
)

func TestSchedulerRecordsOneActivityEventPerTask(t *testing.T) {
	evt := recordScheduledTask(t, StateDone, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]bool{"success": true}); err != nil {
			t.Errorf("encode failed: %v", err)
		}
	})
	if evt.Source != "scheduler" {
		t.Errorf("source = %q, want scheduler", evt.Source)
	}
	if evt.AgentID != "agent-1" {
		t.Errorf("agentId = %q, want agent-1", evt.AgentID)
	}
	if evt.TabID != "tab-1" {
		t.Errorf("tabId = %q, want tab-1", evt.TabID)
	}
	if evt.Action != "click" {
		t.Errorf("action = %q, want click", evt.Action)
	}
	if evt.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", evt.Status)
	}
}

func TestSchedulerRecordsTheInstanceStatusOnFailure(t *testing.T) {
	evt := recordScheduledTask(t, StateFailed, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "selector matched no element", http.StatusNotFound)
	})
	if evt.Status != http.StatusNotFound {
		t.Errorf("status = %d, want the instance's 404, not a flat 502", evt.Status)
	}
	if !strings.Contains(evt.Error, "selector matched no element") {
		t.Errorf("error = %q, want the instance's body", evt.Error)
	}
}

func recordScheduledTask(t *testing.T, want TaskState, instanceHandler http.HandlerFunc) activity.Event {
	t.Helper()
	instance := httptest.NewServer(instanceHandler)
	defer instance.Close()

	parts := strings.Split(instance.URL, ":")
	port := parts[len(parts)-1]

	rec, err := activity.NewRecorder(activity.Config{
		Enabled:       true,
		RetentionDays: 1,
		Events:        activity.EventSourceConfig{Scheduler: true},
	}, t.TempDir())
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	cfg := DefaultConfig()
	cfg.WorkerCount = 1
	s := New(cfg, &mockResolver{port: port}, rec)
	s.Start()
	defer s.Stop()

	task, err := s.Submit(SubmitRequest{
		AgentID: "agent-1",
		Action:  "click",
		TabID:   "tab-1",
		Ref:     "e14",
	})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		if got := s.GetTask(task.ID); got != nil && got.GetState() == want {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("task did not complete in time")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}

	var events []activity.Event
	poll := time.After(2 * time.Second)
	for {
		events, err = rec.Query(activity.Filter{Sources: []string{"scheduler"}})
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if len(events) > 0 {
			break
		}
		select {
		case <-poll:
			t.Fatalf("no scheduler activity event recorded")
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}

	if len(events) != 1 {
		t.Fatalf("scheduler activity events = %d, want exactly 1", len(events))
	}
	return events[0]
}
