package profiles

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pinchtab/pinchtab/internal/httpx/httpxtest"
)

func TestThePruneRequestDecodesAnOptionalBodyLikeEveryOtherSite(t *testing.T) {
	for _, c := range httpxtest.OptionalBodyCases() {
		t.Run(c.Name, func(t *testing.T) {
			mux := http.NewServeMux()
			NewProfileManager(t.TempDir()).RegisterHandlers(mux)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, c.Request(http.MethodPost, "/profiles/prune", `{"confirm":false}`))

			c.Check(t, true, w)
		})
	}
}
