package clientconnector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGrokDeviceIdentityPersistsAndRotates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TOKHUB_CLIENT_CONNECTOR_HOME", home)
	first, err := EnsureGrokDeviceIdentity()
	if err != nil {
		t.Fatal(err)
	}
	again, err := EnsureGrokDeviceIdentity()
	if err != nil || again != first {
		t.Fatalf("identity did not persist: first=%q again=%q err=%v", first, again, err)
	}
	rotated, err := RotateGrokDeviceIdentity()
	if err != nil || rotated == first {
		t.Fatalf("identity did not rotate: first=%q rotated=%q err=%v", first, rotated, err)
	}
	info, err := os.Stat(filepath.Join(home, grokDeviceIdentityFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("identity file permissions = %v", info.Mode().Perm())
	}
	if err := RemoveGrokDeviceIdentity(); err != nil {
		t.Fatal(err)
	}
}
