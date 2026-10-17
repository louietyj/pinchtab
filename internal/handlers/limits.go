package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/pinchtab/pinchtab/internal/httpx"
)

const maxBodySize = httpx.DefaultMaxJSONBodyBytes

func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodySize)).Decode(dst)
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	httpx.Error(w, httpx.StatusForJSONDecodeError(err), fmt.Errorf("decode: %w", err))
	return false
}
