package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSAllowsCurrentRequestOriginWhenPublicURLDiffers(t *testing.T) {
	s := &Server{cfg: Config{PublicURL: "http://localhost:8080"}}
	handler := s.cors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://localhost:28080/assets/index.js", nil)
	req.Host = "localhost:28080"
	req.Header.Set("Origin", "http://localhost:28080")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 body=%q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:28080" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want current origin", got)
	}
}
