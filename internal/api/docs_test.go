package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAPISpecServesYAML(t *testing.T) {
	docsDir := t.TempDir()
	spec := "openapi: 3.1.0\ninfo:\n  title: TokHub API\n"
	if err := os.WriteFile(filepath.Join(docsDir, "openapi.yaml"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &Server{cfg: Config{DocsDir: docsDir}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)

	s.openAPISpec(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/yaml") {
		t.Fatalf("expected yaml content type, got %q", got)
	}
	if !strings.Contains(rec.Body.String(), "openapi: 3.1.0") {
		t.Fatalf("expected OpenAPI body, got %q", rec.Body.String())
	}
}

func TestOpenAPISpecMissingReturnsControlledError(t *testing.T) {
	s := &Server{cfg: Config{DocsDir: t.TempDir()}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)

	s.openAPISpec(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "openapi_not_found") {
		t.Fatalf("expected controlled error, got %q", rec.Body.String())
	}
}
