package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pinchtab/pinchtab/internal/bridge"
	"github.com/pinchtab/pinchtab/internal/config"
)

// Closing a tab classifies the failure the way every other tab-scoped op does: a
// missing tab is a 404 client error, the last-tab precondition a 409, and only a
// genuine internal failure a 500. Before this fix the explicit-tabID path mapped
// every CloseTab error to 500, so a missing tab tripped integrator alerting.
func TestCloseTabClassifiesBridgeErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{name: "missing tab is 404", err: &bridge.TabNotFoundError{TabID: "DEADBEEF00000000"}, want: http.StatusNotFound},
		{name: "last tab precondition is 409", err: bridge.ErrCannotCloseLastTab, want: http.StatusConflict},
		{name: "internal failure stays 500", err: fmt.Errorf("tab manager not initialized"), want: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := New(&mockBridge{closeTabErr: tc.err}, &config.RuntimeConfig{}, nil, nil, nil)
			mux := http.NewServeMux()
			h.RegisterRoutes(mux, nil)

			req := httptest.NewRequest(http.MethodPost, "/tabs/DEADBEEF00000000/close", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

// The success path is unchanged: a real close still returns 200.
func TestCloseTabSucceeds(t *testing.T) {
	h := New(&mockBridge{}, &config.RuntimeConfig{}, nil, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, nil)

	req := httptest.NewRequest(http.MethodPost, "/close", bytes.NewReader([]byte(`{"tabId":"tab1"}`)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

// The last-tab guard returns a sentinel handlers can classify with errors.Is,
// not a message they must string-match — so rewording it cannot silently send
// the precondition back to 500.
func TestErrCannotCloseLastTabIsMatchable(t *testing.T) {
	if !errors.Is(fmt.Errorf("wrap: %w", bridge.ErrCannotCloseLastTab), bridge.ErrCannotCloseLastTab) {
		t.Fatal("ErrCannotCloseLastTab is not matchable through errors.Is")
	}
}
