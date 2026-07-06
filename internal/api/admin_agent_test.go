package api

import (
	"net/http"
	"reflect"
	"testing"
)

func TestAdminAgentRequiredScopes(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		want   []string
	}{
		{
			name:   "read admin data",
			method: http.MethodGet,
			path:   "/api/admin/channels",
			want:   []string{"admin:read"},
		},
		{
			name:   "export admin data",
			method: http.MethodGet,
			path:   "/api/admin/audit/export",
			want:   []string{"admin:read", "admin:export"},
		},
		{
			name:   "export platform channels includes secrets",
			method: http.MethodPost,
			path:   "/api/admin/channels/export",
			want:   []string{"admin:read", "admin:write", "admin:secrets", "admin:export"},
		},
		{
			name:   "import platform channels writes secrets",
			method: http.MethodPost,
			path:   "/api/admin/channels/import",
			want:   []string{"admin:write", "admin:dangerous", "admin:secrets"},
		},
		{
			name:   "sync platform channels writes secrets",
			method: http.MethodPost,
			path:   "/api/admin/channels/sync",
			want:   []string{"admin:write", "admin:dangerous", "admin:secrets"},
		},
		{
			name:   "create key returns secret",
			method: http.MethodPost,
			path:   "/api/admin/gateway-keys",
			want:   []string{"admin:write", "admin:secrets"},
		},
		{
			name:   "delete is dangerous",
			method: http.MethodDelete,
			path:   "/api/admin/gateways/gw_1",
			want:   []string{"admin:write", "admin:dangerous"},
		},
		{
			name:   "patch user can affect password",
			method: http.MethodPatch,
			path:   "/api/admin/users/usr_1",
			want:   []string{"admin:write", "admin:secrets"},
		},
		{
			name:   "patch user status is ordinary write",
			method: http.MethodPatch,
			path:   "/api/admin/users/usr_1/status",
			want:   []string{"admin:write"},
		},
		{
			name:   "download package is export dangerous and secret-bearing",
			method: http.MethodGet,
			path:   "/api/admin/channel-sites/site_1/download",
			want:   []string{"admin:read", "admin:dangerous", "admin:secrets", "admin:export"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := adminAgentRequiredScopes(tc.method, tc.path)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("adminAgentRequiredScopes(%q, %q) = %#v, want %#v", tc.method, tc.path, got, tc.want)
			}
		})
	}
}

func TestAdminAgentNeedsWriteGuard(t *testing.T) {
	if !adminAgentNeedsWriteGuard(http.MethodPost, "/api/admin/channels/ch_1/validate") {
		t.Fatal("expected mutating request to require reason and idempotency key")
	}
	if adminAgentNeedsWriteGuard(http.MethodGet, "/api/admin/production-health") {
		t.Fatal("expected ordinary GET request to skip write guard")
	}
	if !adminAgentNeedsWriteGuard(http.MethodGet, "/api/admin/channel-sites/site_1/download") {
		t.Fatal("expected package download to require reason and idempotency key")
	}
}

func TestMissingAdminAgentScopes(t *testing.T) {
	got := missingAdminAgentScopes([]string{"admin:read", "admin:write"}, []string{"admin:write", "admin:dangerous"})
	want := []string{"admin:dangerous"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missingAdminAgentScopes = %#v, want %#v", got, want)
	}
}
