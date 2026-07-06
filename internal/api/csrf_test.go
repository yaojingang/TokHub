package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRFAllowsPublicRecommendClickWithoutToken(t *testing.T) {
	s := &Server{}
	called := false
	handler := s.csrf(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/public/recommend/click", strings.NewReader(`{"itemType":"cta","itemId":"smoke"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected public recommend click to bypass CSRF")
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
}

func TestCSRFRejectsNonPostPublicRecommendClickWithoutToken(t *testing.T) {
	s := &Server{}
	handler := s.csrf(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("non-POST public recommend click reached handler without CSRF token")
	}))

	req := httptest.NewRequest(http.MethodPatch, "/api/public/recommend/click", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "csrf_invalid") {
		t.Fatalf("expected csrf error, got %q", rec.Body.String())
	}
}

func TestCSRFRejectsPrivateWriteWithoutToken(t *testing.T) {
	s := &Server{}
	handler := s.csrf(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("private write reached handler without CSRF token")
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/me/private-channels", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "csrf_invalid") {
		t.Fatalf("expected csrf error, got %q", rec.Body.String())
	}
}

func TestCSRFAllowsAdminAgentBearerWriteWithoutToken(t *testing.T) {
	s := &Server{}
	called := false
	handler := s.csrf(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/admin/channels", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer aat_test")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected admin agent bearer write to bypass CSRF")
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
}

func TestCSRFDoesNotBypassNonAdminBearerWrite(t *testing.T) {
	s := &Server{}
	handler := s.csrf(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("non-admin bearer write reached handler without CSRF token")
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/me/private-channels", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer aat_test")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "csrf_invalid") {
		t.Fatalf("expected csrf error, got %q", rec.Body.String())
	}
}
