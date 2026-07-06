package store

import "testing"

func TestNormalizeOpenAPIScopesDefaultExcludesChannelSync(t *testing.T) {
	scopes, err := normalizeOpenAPIScopes(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range scopes {
		if scope == "channel_sync" {
			t.Fatalf("default Open API scopes include channel_sync: %#v", scopes)
		}
	}
}

func TestNormalizeOpenAPIScopesAllowsExplicitChannelSync(t *testing.T) {
	scopes, err := normalizeOpenAPIScopes([]string{"channel_sync"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 1 || scopes[0] != "channel_sync" {
		t.Fatalf("scopes = %#v, want channel_sync only", scopes)
	}
}
