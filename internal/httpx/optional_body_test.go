package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pinchtab/pinchtab/internal/httpx/httpxtest"
)

func TestDecodeOptionalJSONBodyDecodesEachBodyShape(t *testing.T) {
	type payload struct {
		URL string `json:"url"`
	}
	for _, c := range httpxtest.OptionalBodyCases() {
		t.Run(c.Name, func(t *testing.T) {
			var got payload
			w := httptest.NewRecorder()

			err := DecodeOptionalJSONBody(w, c.Request(http.MethodPost, "/", `{"url":"https://example.com"}`), 0, &got)

			switch c.Strict {
			case httpxtest.Proceeds:
				want := payload{}
				if c.Name == "chunked valid payload" {
					want.URL = "https://example.com"
				}
				if err != nil || got != want {
					t.Fatalf("decoded %+v, %v; want %+v, nil", got, err, want)
				}
			case httpxtest.BadRequest:
				if err == nil || StatusForJSONDecodeError(err) != http.StatusBadRequest {
					t.Fatalf("err = %v, want a 400 decode error", err)
				}
			case httpxtest.TooLarge:
				if err == nil || StatusForJSONDecodeError(err) != http.StatusRequestEntityTooLarge {
					t.Fatalf("err = %v, want a 413 decode error", err)
				}
			}
			if w.Body.Len() != 0 || w.Code != http.StatusOK {
				t.Fatalf("the helper wrote a response (%d %q); its callers own that", w.Code, w.Body.String())
			}
		})
	}
}
