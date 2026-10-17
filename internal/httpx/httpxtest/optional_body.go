package httpxtest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type Outcome int

const (
	Proceeds Outcome = iota
	BadRequest
	TooLarge
)

type OptionalBodyCase struct {
	Name    string
	Body    string
	Chunked bool
	Absent  bool
	Strict  Outcome
	Lenient Outcome
	payload bool
}

func OptionalBodyCases() []OptionalBodyCase {
	return []OptionalBodyCase{
		{Name: "no body", Absent: true, Strict: Proceeds, Lenient: Proceeds},
		{Name: "empty chunked body", Chunked: true, Strict: Proceeds, Lenient: Proceeds},
		{Name: "chunked valid payload", Chunked: true, payload: true, Strict: Proceeds, Lenient: Proceeds},
		{Name: "malformed JSON", Body: `{"a":`, Strict: BadRequest, Lenient: BadRequest},
		{Name: "body over 1 MiB", Body: `{"pad":"` + strings.Repeat("x", 1<<20) + `"}`, Strict: TooLarge, Lenient: TooLarge},
		{Name: "unknown field", Body: `{"zzUnknownField":1}`, Strict: BadRequest, Lenient: Proceeds},
	}
}

func (c OptionalBodyCase) Request(method, target, payload string) *http.Request {
	if c.Absent {
		return httptest.NewRequest(method, target, http.NoBody)
	}
	body := c.Body
	if c.payload {
		body = payload
	}
	req := httptest.NewRequest(method, target, io.NopCloser(strings.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	if c.Chunked {
		req.ContentLength = -1
		req.TransferEncoding = []string{"chunked"}
	}
	return req
}

var decodeRefusalMarkers = []string{"decode:", "unexpected EOF", "invalid character", "unknown field", "request body too large", "cannot unmarshal"}

func refusedByDecode(body string) bool {
	for _, marker := range decodeRefusalMarkers {
		if strings.Contains(body, marker) {
			return true
		}
	}
	return false
}

func (c OptionalBodyCase) Check(t *testing.T, strict bool, w *httptest.ResponseRecorder) {
	t.Helper()
	want := c.Lenient
	if strict {
		want = c.Strict
	}
	body := w.Body.String()
	if strings.Contains(body, "404 page not found") {
		t.Fatalf("%s: the request never reached a handler", c.Name)
	}
	switch want {
	case Proceeds:
		if w.Code == http.StatusRequestEntityTooLarge || refusedByDecode(body) {
			t.Fatalf("%s: the body decode refused it (%d): %s", c.Name, w.Code, body)
		}
	case BadRequest:
		if w.Code != http.StatusBadRequest || !refusedByDecode(body) {
			t.Fatalf("%s: got %d %s, want a 400 decode refusal", c.Name, w.Code, body)
		}
	case TooLarge:
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("%s: got %d %.200s, want 413", c.Name, w.Code, body)
		}
	}
}
