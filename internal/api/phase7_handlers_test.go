package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIncidentActionMessageAllowsChunkedEmptyBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/admin/incidents/inc_1/resolve", strings.NewReader(""))
	req.ContentLength = -1
	rec := httptest.NewRecorder()

	message, ok := incidentActionMessage(rec, req)
	if !ok {
		t.Fatalf("expected empty chunked body to be accepted, got status %d body %q", rec.Code, rec.Body.String())
	}
	if message != "" {
		t.Fatalf("message = %q, want empty", message)
	}
}
