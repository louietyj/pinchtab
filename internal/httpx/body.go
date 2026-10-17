package httpx

import (
	"bytes"
	"io"
	"net/http"
)

func MayHaveBody(r *http.Request) bool {
	return r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0
}

type replayedBody struct {
	io.Reader
	io.Closer
}

func ReplayBody(peeked []byte, original io.ReadCloser) io.ReadCloser {
	return replayedBody{Reader: io.MultiReader(bytes.NewReader(peeked), original), Closer: original}
}
