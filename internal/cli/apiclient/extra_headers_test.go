package apiclient

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtraHeadersParsesLinesAndSkipsMalformedOnes(t *testing.T) {
	t.Setenv(ExtraHeadersEnv, "CF-Access-Client-Id: abc.access\n\nnot a header\n CF-Access-Client-Secret :s3cr:et \n")
	h := ExtraHeaders()
	if got := h.Get("Cf-Access-Client-Id"); got != "abc.access" {
		t.Errorf("client id = %q", got)
	}
	if got := h.Get("Cf-Access-Client-Secret"); got != "s3cr:et" {
		t.Errorf("client secret = %q, want the value after the first colon only", got)
	}
	if len(h) != 2 {
		t.Errorf("got %d headers, want 2: %v", len(h), h)
	}
}

func TestHeaderTransportAddsExtraHeaders(t *testing.T) {
	t.Setenv(ExtraHeadersEnv, "X-One: 1")
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-One")
	}))
	defer srv.Close()

	client := &http.Client{Transport: HeaderTransport{}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got != "1" {
		t.Errorf("X-One = %q, want 1", got)
	}
}
