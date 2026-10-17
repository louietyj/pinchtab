package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pinchtab/pinchtab/internal/authn"
	"github.com/pinchtab/pinchtab/internal/config"
)

func TestHandleScreencast_AuthRejectsNoToken(t *testing.T) {
	cfg := &config.RuntimeConfig{Token: "secret-token-123", AllowScreencast: true}
	h := New(&mockBridge{}, cfg, nil, nil, nil)

	req := httptest.NewRequest("GET", "/screencast", nil)
	w := httptest.NewRecorder()
	handler := AuthMiddleware(config.NewLive(cfg), http.HandlerFunc(h.HandleScreencast))
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized without token, got %d", w.Code)
	}
}

func TestHandleScreencast_AuthRejectsWrongToken(t *testing.T) {
	cfg := &config.RuntimeConfig{Token: "secret-token-123", AllowScreencast: true}
	h := New(&mockBridge{}, cfg, nil, nil, nil)

	req := httptest.NewRequest("GET", "/screencast", nil)
	req.AddCookie(&http.Cookie{Name: authn.CookieName, Value: "wrong-token"})
	req.Header.Set("Referer", "http://example.com/dashboard")
	w := httptest.NewRecorder()
	handler := AuthMiddleware(config.NewLive(cfg), http.HandlerFunc(h.HandleScreencast))
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized with wrong token, got %d", w.Code)
	}
}

func TestHandleScreencast_AuthRejectsWrongHeader(t *testing.T) {
	cfg := &config.RuntimeConfig{Token: "secret-token-123", AllowScreencast: true}
	h := New(&mockBridge{}, cfg, nil, nil, nil)

	req := httptest.NewRequest("GET", "/screencast", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()
	handler := AuthMiddleware(config.NewLive(cfg), http.HandlerFunc(h.HandleScreencast))
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized with wrong Bearer header, got %d", w.Code)
	}
}

func TestHandleScreencast_NoTokenConfigRejectsRequest(t *testing.T) {
	cfg := &config.RuntimeConfig{} // No token configured
	h := New(&mockBridge{failTab: true}, cfg, nil, nil, nil)

	req := httptest.NewRequest("GET", "/screencast", nil)
	w := httptest.NewRecorder()
	handler := AuthMiddleware(config.NewLive(cfg), http.HandlerFunc(h.HandleScreencast))
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when no token is configured, got %d", w.Code)
	}
}

func TestHandleScreencast_Disabled(t *testing.T) {
	cfg := &config.RuntimeConfig{}
	h := New(&mockBridge{}, cfg, nil, nil, nil)
	req := httptest.NewRequest("GET", "/screencast", nil)
	w := httptest.NewRecorder()
	h.HandleScreencast(w, req)
	if w.Code != 403 {
		t.Errorf("expected 403 when screencast disabled, got %d", w.Code)
	}
}

func TestHandleScreencast_TabNotFoundUsesTheSharedTabContextError(t *testing.T) {
	cfg := &config.RuntimeConfig{AllowScreencast: true}
	h := New(&mockBridge{failTab: true}, cfg, nil, nil, nil)
	req := httptest.NewRequest("GET", "/screencast?tabId=missing", nil)
	w := httptest.NewRecorder()

	h.HandleScreencast(w, req)

	want := httptest.NewRecorder()
	WriteTabContextError(want, errors.New("tab not found"), http.StatusNotFound)
	if w.Code != http.StatusNotFound || w.Body.String() != want.Body.String() {
		t.Fatalf("got %d %s, want the shared tab-context 404 %s", w.Code, w.Body.String(), want.Body.String())
	}
}
