package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pinchtab/pinchtab/internal/httpx"
	"github.com/pinchtab/pinchtab/internal/httpx/httpxtest"
)

func TestDecodeOptionalJSONDecodesEachBodyShapeLeniently(t *testing.T) {
	type payload struct {
		TabID string `json:"tabId"`
	}
	for _, c := range httpxtest.OptionalBodyCases() {
		t.Run(c.Name, func(t *testing.T) {
			var got payload
			w := httptest.NewRecorder()

			ok := decodeOptionalJSON(w, c.Request(http.MethodPost, "/", `{"tabId":"tab1"}`), &got)

			want := payload{}
			if c.Name == "chunked valid payload" {
				want.TabID = "tab1"
			}
			if proceeds := c.Lenient == httpxtest.Proceeds; ok != proceeds || (ok && got != want) {
				t.Fatalf("ok=%v decoded %+v, want ok=%v %+v", ok, got, proceeds, want)
			}
			c.Check(t, false, w)
		})
	}
}

func TestMaxBodySizeIsTheSharedJSONBodyLimit(t *testing.T) {
	if maxBodySize != httpx.DefaultMaxJSONBodyBytes {
		t.Fatalf("maxBodySize = %d, want httpx.DefaultMaxJSONBodyBytes (%d)", maxBodySize, httpx.DefaultMaxJSONBodyBytes)
	}
}
