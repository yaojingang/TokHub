package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCleanOfficialClientSessionsHonorsRetentionAndLeavesAuth(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, "codex")
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	oldCodex := filepath.Join(codexHome, "sessions", "old.jsonl")
	recentCodex := filepath.Join(codexHome, "sessions", "recent.jsonl")
	oldGrok := filepath.Join(home, ".grok", "sessions", "old.json")
	auth := filepath.Join(home, ".grok", "auth.json")
	for _, path := range []string{oldCodex, recentCodex, oldGrok, auth} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := now.Add(-25 * time.Hour)
	for _, path := range []string{oldCodex, oldGrok, auth} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	recent := now.Add(-time.Hour)
	if err := os.Chtimes(recentCodex, recent, recent); err != nil {
		t.Fatal(err)
	}
	removed, err := cleanOfficialClientSessions(now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("expected two expired session files, removed %d", removed)
	}
	for _, path := range []string{oldCodex, oldGrok} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expired session file still exists: %s", path)
		}
	}
	for _, path := range []string{recentCodex, auth} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("protected file was removed: %s: %v", path, err)
		}
	}
}

func TestOfficialLoginCommandsUseProviderDeviceAuth(t *testing.T) {
	tests := []struct {
		provider string
		command  string
		args     []string
	}{
		{provider: "chatgpt", command: "codex", args: []string{"login", "--device-auth"}},
		{provider: "codex", command: "codex", args: []string{"login", "--device-auth"}},
		{provider: "grok", command: "grok", args: []string{"login", "--device-auth"}},
	}
	for _, test := range tests {
		command, args, _, err := officialLoginCommand(test.provider)
		if err != nil || command != test.command || !reflect.DeepEqual(args, test.args) {
			t.Fatalf("login command for %s = %s %#v, err=%v", test.provider, command, args, err)
		}
	}
	if _, _, _, err := officialLoginCommand("deepseek"); err == nil {
		t.Fatal("unsupported provider received an official login command")
	}
}

func TestLogoutSessionCleanupPreservesProviderAuth(t *testing.T) {
	home := t.TempDir()
	codexHome := filepath.Join(home, "codex")
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)
	paths := []string{
		filepath.Join(codexHome, "sessions", "thread.jsonl"),
		filepath.Join(home, ".grok", "sessions", "session.json"),
		filepath.Join(codexHome, "auth.json"),
		filepath.Join(home, ".grok", "auth.json"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := clearOfficialClientSessions("chatgpt"); err != nil {
		t.Fatal(err)
	}
	if err := clearOfficialClientSessions("grok"); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths[:2] {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("session was retained after logout cleanup: %s", path)
		}
	}
	for _, path := range paths[2:] {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("auth state was deleted by session cleanup: %s: %v", path, err)
		}
	}
}
